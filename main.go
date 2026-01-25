package main

import (
	"bytes"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	filedialog "github.com/sqweek/dialog"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var logEntry *widget.Entry

func logAppend(s string) {
	current := logEntry.Text
	logEntry.SetText(current + s + "\n")
	logEntry.CursorRow = strings.Count(logEntry.Text, "\n") + 5
	logEntry.Refresh()
}

func humanSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	suffix := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	s := float64(size)
	i := 0
	for s >= 1024 && i < len(suffix)-1 {
		s /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", s, suffix[i])
}

func fixPartitionTableOnFile(path string, w fyne.Window) {
	logAppend("\n────────────────────────────────────────")
	logAppend(fmt.Sprintf("Selected file: %s", path))

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		logAppend(fmt.Sprintf("Cannot open file for read/write: %v", err))
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		logAppend(fmt.Sprintf("Cannot stat file: %v", err))
		return
	}

	if fi.Size() < 512 {
		logAppend("File is too small (< 512 bytes) — not a disk image?")
		return
	}

	logAppend(fmt.Sprintf("File size: %s", humanSize(fi.Size())))

	mbr := make([]byte, 512)
	_, err = f.ReadAt(mbr, 0)
	if err != nil && err != io.EOF {
		logAppend(fmt.Sprintf("Failed to read first 512 bytes: %v", err))
		return
	}

	mbr2 := make([]byte, 512)
	_, err = f.ReadAt(mbr2, 512)
	if err != nil && err != io.EOF {
		logAppend(fmt.Sprintf("Failed to read second 512 bytes: %v", err))
		return
	}

	if mbr[510] != 0x55 || mbr[511] != 0xAA {
		logAppend("No valid MBR signature (55 AA) found!")
		return
	}

	logAppend("MBR1 signature found")

	if mbr2[510] != 0x55 || mbr2[511] != 0xAA {
		logAppend("No valid MBR signature (55 AA) found!")
		return
	}

	logAppend("MBR2 signature found")

	expected := []byte("CLARION ID")
	if len(mbr2) < 10 || !bytes.Equal(mbr2[0:10], expected) {
		logAppend("ERROR: Could not find Clarion signature from MBR2. The disk image is likely not valid or corrupted backup.")
		return
	}

	logAppend("Found Clarion signature!\nCopying MBR..")

	newMbr1 := make([]byte, 512)

	copy(newMbr1, mbr2)
	// zero out clarion id
	copy(newMbr1, make([]byte, 10))

	_, err = f.WriteAt(newMbr1, 0)
	if err != nil {
		logAppend(fmt.Sprintf("Cannot write file: %v", err))
		return
	}

	logAppend("\nDone. You can now burn this disk image to a secondary SD Card and hidden system partitions will be visible. PLEASE always keep a backup of the original SD Card just in case!")
	logAppend("────────────────────────────────────────")
}

func main() {
	a := app.NewWithID("lst_comp")
	w := a.NewWindow("Leaf SD Tools Companion")

	fixButton := widget.NewButton("Select image file and fix partition table", nil)

	statusLabel := widget.NewLabel("No file selected yet")
	statusLabel.Wrapping = fyne.TextWrapWord

	fixTabContent := container.NewVBox(
		widget.NewLabel("Fix Partition Table on disk image to make hidden system partitions visible"),
		widget.NewLabel(""),
		statusLabel,
		fixButton,
	)

	patchesContent := container.NewVBox(
		widget.NewLabel("Patches can enable various functionality and/or unlock existing functions."),
		widget.NewLabel("TODO! Not ready yet"),
	)

	fixButton.OnTapped = func() {
		filename, err := filedialog.File().Filter("Disk Image File", "img").Load()
		if err != nil {
			logAppend("File open dialog error: " + err.Error())
			return
		}
		statusLabel.SetText("Selected: " + filepath.Base(filename))
		fixPartitionTableOnFile(filename, w)
	}

	tabs := container.NewAppTabs(
		container.NewTabItem("Fix Partition Table", fixTabContent),
		container.NewTabItem("Patches", patchesContent),
	)

	logEntry = widget.NewMultiLineEntry()
	logEntry.Wrapping = fyne.TextWrapWord
	logEntry.TextStyle = fyne.TextStyle{Monospace: true}
	logEntry.SetMinRowsVisible(5)

	container.NewPadded(tabs)

	mainContent := container.NewBorder(
		tabs,
		nil,
		nil,
		nil,
		logEntry,
	)

	padded := container.NewPadded(mainContent)
	w.SetContent(padded)

	w.Resize(fyne.NewSize(800, 480))

	logAppend("LeafSDTools Companion v1.0.0")
	logAppend("https://github.com/developerfromjokela/leafsdtools_companion")
	logAppend("\nNOTE — ALWAYS backup your original SD card first before messing with it!")

	w.ShowAndRun()
}
