package main

import (
	"LeafSDTools_Companion/disk"
	"LeafSDTools_Companion/privilege"
	"LeafSDTools_Companion/utils"
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	filedialog "github.com/sqweek/dialog"
)

var logEntry *widget.Entry

func logAppend(s string) {
	fyne.Do(func() {
		if logEntry == nil {
			return
		}
		current := logEntry.Text
		logEntry.SetText(current + s + "\n")
		logEntry.CursorRow = strings.Count(logEntry.Text, "\n") + 5
		logEntry.Refresh()
	})
}

type greenPrimaryTheme struct{ fyne.Theme }

func (t greenPrimaryTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if n == theme.ColorNamePrimary {
		return color.NRGBA{R: 0x27, G: 0xae, B: 0x60, A: 0xff}
	}
	return t.Theme.Color(n, v)
}

func fixPartitionTable(path string, isDevice bool) {
	logAppend("\n────────────────────────────────────────")
	if isDevice {
		logAppend(fmt.Sprintf("Target device: %s", path))
		logAppend("Unmounting volumes...")
		if err := disk.UnmountDevice(path); err != nil {
			logAppend("Unmount warning: " + err.Error())
		}
	} else {
		logAppend(fmt.Sprintf("Target image: %s", path))
	}

	rw, size, err := disk.OpenDeviceForReadWrite(path)
	if err != nil {
		logAppend(fmt.Sprintf("Cannot open for read/write: %v", err))
		return
	}
	defer rw.Close()

	type ra interface {
		ReadAt([]byte, int64) (int, error)
		WriteAt([]byte, int64) (int, error)
	}
	f, ok := rw.(ra)
	if !ok {
		logAppend("Opened target does not support random access")
		return
	}

	if size > 0 && size < 1024 {
		logAppend("Target is too small (< 1024 bytes)")
		return
	}
	if size > 0 {
		logAppend(fmt.Sprintf("Size: %s", utils.HumanSize(size)))
	}

	mbr := make([]byte, 512)
	if _, err = f.ReadAt(mbr, 0); err != nil && err != io.EOF {
		logAppend(fmt.Sprintf("Failed to read first 512 bytes: %v", err))
		return
	}
	mbr2 := make([]byte, 512)
	if _, err = f.ReadAt(mbr2, 512); err != nil && err != io.EOF {
		logAppend(fmt.Sprintf("Failed to read second 512 bytes: %v", err))
		return
	}

	if mbr[510] != 0x55 || mbr[511] != 0xAA {
		logAppend("No valid MBR signature (55 AA) at sector 0")
		return
	}
	logAppend("MBR1 signature found")

	if mbr2[510] != 0x55 || mbr2[511] != 0xAA {
		logAppend("No valid MBR signature (55 AA) at sector 1")
		return
	}
	logAppend("MBR2 signature found")

	expected := []byte("CLARION ID")
	if len(mbr2) < 10 || !bytes.Equal(mbr2[0:10], expected) {
		logAppend("ERROR: Clarion signature not found in MBR2, not a valid Leaf/Clarion backup?")
		return
	}
	logAppend("Found Clarion signature, copying MBR2 → MBR1...")

	newMbr1 := make([]byte, 512)
	copy(newMbr1, mbr2)
	copy(newMbr1, make([]byte, 10)) // zero Clarion ID

	if _, err = f.WriteAt(newMbr1, 0); err != nil {
		logAppend(fmt.Sprintf("Cannot write MBR: %v", err))
		return
	}

	logAppend("Done. Hidden system partitions should now be visible after re-insert / remount.")
	logAppend("────────────────────────────────────────")
}

func buildFixTab() fyne.CanvasObject {
	status := widget.NewLabel("Choose a target, then apply the fix.")
	status.Wrapping = fyne.TextWrapWord

	var imagePath string
	var selectedDevice disk.Device
	deviceByLabel := map[string]disk.Device{}

	imageLabel := widget.NewLabel("No image selected")
	imageLabel.Wrapping = fyne.TextWrapWord

	deviceSelect := widget.NewSelect(nil, nil)
	deviceSelect.PlaceHolder = "Select device..."
	deviceSelect.Hide()

	refreshDevices := func() {
		list, err := disk.ListDevices()
		if err != nil {
			logAppend("Failed to list devices: " + err.Error())
			return
		}
		deviceByLabel = map[string]disk.Device{}
		opts := make([]string, len(list))
		for i, d := range list {
			opts[i] = d.String()
			deviceByLabel[d.String()] = d
		}
		deviceSelect.Options = opts
		deviceSelect.ClearSelected()
		deviceSelect.Refresh()
	}

	chooseImageBtn := widget.NewButton("Choose .img file...", func() {
		filename, err := filedialog.File().Filter("Disk Image", "img").Load()
		if err != nil {
			logAppend("Open dialog error: " + err.Error())
			return
		}
		if filename == "" {
			return
		}
		imagePath = filename
		imageLabel.SetText(filepath.Base(filename))
	})

	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), refreshDevices)
	refreshBtn.Hide()

	deviceRow := container.NewBorder(nil, nil, nil, refreshBtn, deviceSelect)
	deviceRow.Hide()

	modeRadio := widget.NewRadioGroup([]string{"Disk image (.img)", "Physical device"}, func(s string) {
		switch s {
		case "Disk image (.img)":
			chooseImageBtn.Show()
			imageLabel.Show()
			deviceRow.Hide()
			deviceSelect.Hide()
			refreshBtn.Hide()
		case "Physical device":
			chooseImageBtn.Hide()
			imageLabel.Hide()
			deviceRow.Show()
			deviceSelect.Show()
			refreshBtn.Show()
			refreshDevices()
		}
	})
	modeRadio.SetSelected("Disk image (.img)")
	modeRadio.Horizontal = true

	applyBtn := widget.NewButton("Apply fix", func() {
		if modeRadio.Selected == "Physical device" {
			d, ok := deviceByLabel[deviceSelect.Selected]
			if !ok {
				logAppend("Please select a device first.")
				return
			}
			selectedDevice = d
			status.SetText("Device: " + d.String())
			go fixPartitionTable(selectedDevice.Path, true)
			return
		}
		if imagePath == "" {
			logAppend("Please choose an image file first.")
			return
		}
		status.SetText("Image: " + filepath.Base(imagePath))
		go fixPartitionTable(imagePath, false)
	})
	applyBtn.Importance = widget.HighImportance

	return container.NewVBox(
		widget.NewLabelWithStyle("Fix partition table", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Makes hidden Clarion/Leaf system partitions visible."),
		widget.NewSeparator(),
		modeRadio,
		chooseImageBtn,
		imageLabel,
		deviceRow,
		layout.NewSpacer(),
		status,
		applyBtn,
	)
}

// --- Create / Restore image tabs --------------------------------------------

func buildDevicePicker() (*widget.Select, *map[string]disk.Device, func()) {
	deviceByLabel := map[string]disk.Device{}
	sel := widget.NewSelect(nil, nil)
	sel.PlaceHolder = "Select device..."
	refresh := func() {
		list, err := disk.ListDevices()
		if err != nil {
			logAppend("Failed to list devices: " + err.Error())
			return
		}
		deviceByLabel = map[string]disk.Device{}
		opts := make([]string, len(list))
		for i, d := range list {
			opts[i] = d.String()
			deviceByLabel[d.String()] = d
		}
		sel.Options = opts
		sel.ClearSelected()
		sel.Refresh()
	}
	return sel, &deviceByLabel, refresh
}

func buildBackupTab() fyne.CanvasObject {
	deviceSelect, deviceByLabel, refreshDevices := buildDevicePicker()
	refreshBtn := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), refreshDevices)

	var destPath string
	destLabel := widget.NewLabel("No file chosen")
	destLabel.Wrapping = fyne.TextWrapWord
	chooseDest := widget.NewButton("Save as...", func() {
		filename, err := filedialog.File().Filter("Disk Image", "img").Save()
		if err != nil {
			logAppend("Save dialog error: " + err.Error())
			return
		}
		if filename == "" {
			return
		}
		if !strings.HasSuffix(strings.ToLower(filename), ".img") {
			filename += ".img"
		}
		destPath = filename
		destLabel.SetText(filepath.Base(destPath))
	})

	progress := widget.NewProgressBar()
	status := widget.NewLabel("")
	var cancelChan chan struct{}
	busy := false

	startBtn := widget.NewButton("Start backup", nil)
	cancelBtn := widget.NewButton("Cancel", nil)
	cancelBtn.Disable()
	startBtn.Importance = widget.HighImportance

	startBtn.OnTapped = func() {
		if busy {
			return
		}
		src, ok := (*deviceByLabel)[deviceSelect.Selected]
		if !ok {
			logAppend("Select a source device first.")
			return
		}
		if destPath == "" {
			logAppend("Choose a destination file first.")
			return
		}
		logAppend("\n────────────────────────────────────────")
		logAppend(fmt.Sprintf("Backup %s to %s", src.Path, destPath))
		cancelChan = make(chan struct{})
		busy = true
		startBtn.Disable()
		cancelBtn.Enable()
		progress.SetValue(0)
		status.SetText("Starting...")

		go func() {
			err := disk.CreateDiskImage(src.Path, destPath, 4*1024*1024, func(written, total int64, rate float64) {
				fyne.Do(func() {
					if total > 0 {
						progress.SetValue(float64(written) / float64(total))
						status.SetText(fmt.Sprintf("%s / %s — %s/s", utils.HumanSize(written), utils.HumanSize(total), utils.HumanSize(int64(rate))))
					} else {
						status.SetText(fmt.Sprintf("%s — %s/s", utils.HumanSize(written), utils.HumanSize(int64(rate))))
					}
				})
			}, cancelChan)
			fyne.Do(func() {
				busy = false
				startBtn.Enable()
				cancelBtn.Disable()
				if err != nil {
					logAppend("Backup failed: " + err.Error())
					if errors.Is(err, os.ErrPermission) {
						logAppend("Permission denied, approve the OS prompt and try again.")
					}
				} else {
					progress.SetValue(1)
					logAppend("Backup complete: " + destPath)
				}
				logAppend("────────────────────────────────────────")
			})
		}()
	}
	cancelBtn.OnTapped = func() {
		if cancelChan != nil {
			close(cancelChan)
			cancelChan = nil
		}
	}

	refreshDevices()

	return container.NewVBox(
		widget.NewLabelWithStyle("Backup SD", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		container.NewBorder(nil, nil, nil, refreshBtn, deviceSelect),
		container.NewBorder(nil, nil, nil, chooseDest, destLabel),
		progress,
		status,
		container.NewHBox(startBtn, cancelBtn),
	)
}

func buildRestoreTab() fyne.CanvasObject {
	deviceSelect, deviceByLabel, refreshDevices := buildDevicePicker()
	refreshBtn := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), refreshDevices)

	var imgPath string
	imgLabel := widget.NewLabel("No image chosen")
	imgLabel.Wrapping = fyne.TextWrapWord
	chooseImg := widget.NewButton("Open image...", func() {
		filename, err := filedialog.File().Filter("Disk Image", "img").Load()
		if err != nil {
			logAppend("Open dialog error: " + err.Error())
			return
		}
		if filename == "" {
			return
		}
		imgPath = filename
		imgLabel.SetText(filepath.Base(imgPath))
	})

	verifyCheck := widget.NewCheck("Verify after write", nil)
	verifyCheck.SetChecked(true)

	progress := widget.NewProgressBar()
	status := widget.NewLabel("")
	var cancelChan chan struct{}
	busy := false

	startBtn := widget.NewButton("Start restore", nil)
	cancelBtn := widget.NewButton("Cancel", nil)
	cancelBtn.Disable()
	startBtn.Importance = widget.DangerImportance

	startBtn.OnTapped = func() {
		if busy {
			return
		}
		dst, ok := (*deviceByLabel)[deviceSelect.Selected]
		if !ok {
			logAppend("Select a destination device first.")
			return
		}
		if imgPath == "" {
			logAppend("Choose an image file first.")
			return
		}
		doVerify := verifyCheck.Checked

		warn := fmt.Sprintf(
			"This will OVERWRITE all data on:\n\n  %s\n  %s\n\nwith the image:\n\n  %s\n\nThis cannot be undone. Continue?",
			dst.Name, dst.Path, filepath.Base(imgPath),
		)
		if !filedialog.Message("%s", warn).Title("Confirm restore").YesNo() {
			return
		}

		logAppend("\n────────────────────────────────────────")
		logAppend(fmt.Sprintf("Restore %s to %s", imgPath, dst.Path))
		logAppend("WARNING: All data on the target device will be overwritten.")
		if doVerify {
			logAppend("Verification after write is enabled.")
		}
		cancelChan = make(chan struct{})
		busy = true
		startBtn.Disable()
		cancelBtn.Enable()
		progress.SetValue(0)
		fyne.CurrentApp().Settings().SetTheme(theme.DefaultTheme())
		status.SetText("Writing...")

		phase := "Writing"
		go func() {
			err := disk.RestoreDiskImage(imgPath, dst.Path, 4*1024*1024, doVerify, func(written, total int64, rate float64) {
				fyne.Do(func() {
					if phase == "Writing" && written == 0 && progress.Value > 0.5 {
						phase = "Verifying"
						fyne.CurrentApp().Settings().SetTheme(greenPrimaryTheme{Theme: theme.DefaultTheme()})
						logAppend("Writing done, verifying...")
					}
					label := phase
					if total > 0 {
						progress.SetValue(float64(written) / float64(total))
						status.SetText(fmt.Sprintf("%s: %s / %s — %s/s", label, utils.HumanSize(written), utils.HumanSize(total), utils.HumanSize(int64(rate))))
					} else {
						status.SetText(fmt.Sprintf("%s: %s — %s/s", label, utils.HumanSize(written), utils.HumanSize(int64(rate))))
					}
				})
			}, cancelChan)
			fyne.Do(func() {
				busy = false
				startBtn.Enable()
				cancelBtn.Disable()
				fyne.CurrentApp().Settings().SetTheme(theme.DefaultTheme())
				if err != nil {
					logAppend("Restore failed: " + err.Error())
					if errors.Is(err, os.ErrPermission) {
						logAppend("Permission denied, approve the OS prompt and try again.")
					}
				} else {
					progress.SetValue(1)
					if doVerify {
						logAppend("Restore complete, verification passed.")
					} else {
						logAppend("Restore complete.")
					}
				}
				logAppend("────────────────────────────────────────")
			})
		}()
	}
	cancelBtn.OnTapped = func() {
		if cancelChan != nil {
			close(cancelChan)
			cancelChan = nil
		}
	}

	refreshDevices()

	return container.NewVBox(
		widget.NewLabelWithStyle("Restore backup to SD", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		container.NewBorder(nil, nil, nil, chooseImg, imgLabel),
		container.NewBorder(nil, nil, nil, refreshBtn, deviceSelect),
		verifyCheck,
		progress,
		status,
		container.NewHBox(startBtn, cancelBtn),
	)
}

func main() {
	if privilege.RunHelperIfRequested() {
		return
	}

	a := app.NewWithID("lst_comp")
	w := a.NewWindow("Leaf SD Tools Companion")

	logEntry = widget.NewMultiLineEntry()
	logEntry.Wrapping = fyne.TextWrapWord
	logEntry.TextStyle = fyne.TextStyle{Monospace: true}
	logEntry.SetMinRowsVisible(6)

	tabs := container.NewAppTabs(
		container.NewTabItem("Fix partitions", buildFixTab()),
		container.NewTabItem("Backup", buildBackupTab()),
		container.NewTabItem("Restore", buildRestoreTab()),
		container.NewTabItem("Patches", container.NewVBox(
			widget.NewLabelWithStyle("Patches", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			widget.NewLabel("TODO — not ready yet"),
		)),
	)

	mainContent := container.NewBorder(tabs, nil, nil, nil, logEntry)
	w.SetContent(container.NewPadded(mainContent))
	w.Resize(fyne.NewSize(820, 520))

	logAppend("Leaf SD Tools Companion v1.0.1")
	logAppend("https://github.com/developerfromjokela/leafsdtools_companion")
	logAppend("NOTE — always keep a backup of the original SD card.")

	w.ShowAndRun()
}
