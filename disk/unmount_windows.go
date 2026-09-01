//go:build windows

package disk

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// volumeHold keeps FSCTL_LOCK_VOLUME handles open for the whole write so
// Windows cannot auto-mount partitions as a filesystem appears mid-image.
type volumeHold struct {
	handles []windows.Handle
}

func (h *volumeHold) Close() error {
	if h == nil {
		return nil
	}
	var returned uint32
	for _, handle := range h.handles {
		_ = windows.DeviceIoControl(handle, fsctlUnlockVolume, nil, 0, nil, 0, &returned, nil)
		_ = windows.CloseHandle(handle)
	}
	h.handles = nil
	return nil
}

func holdDeviceVolumesPlatform(devicePath string) (VolumeHold, error) {
	driveNum, err := physicalDriveNumber(devicePath)
	if err != nil {
		return nil, err
	}

	vols, err := volumesOnPhysicalDrive(driveNum)
	if err != nil {
		return nil, err
	}

	hold := &volumeHold{}
	var errs []string
	for _, vol := range vols {
		handle, err := lockAndDismountVolumeHold(vol)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", vol, err))
			continue
		}
		if handle != 0 {
			hold.handles = append(hold.handles, handle)
		}
	}

	_ = setDiskOffline(devicePath, true)

	if len(hold.handles) == 0 && len(vols) > 0 && len(errs) > 0 {
		return nil, fmt.Errorf("could not lock volumes: %s", strings.Join(errs, "; "))
	}
	return hold, nil
}

func unmountDevicePlatform(devicePath string) error {
	driveNum, err := physicalDriveNumber(devicePath)
	if err != nil {
		return err
	}
	vols, err := volumesOnPhysicalDrive(driveNum)
	if err != nil {
		return err
	}
	if len(vols) == 0 {
		return nil
	}
	var errs []string
	for _, vol := range vols {
		h, err := lockAndDismountVolumeHold(vol)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", vol, err))
			continue
		}
		if h != 0 {
			var returned uint32
			_ = windows.DeviceIoControl(h, fsctlUnlockVolume, nil, 0, nil, 0, &returned, nil)
			_ = windows.CloseHandle(h)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("dismount: %s", strings.Join(errs, "; "))
	}
	return nil
}

func physicalDriveNumber(path string) (int, error) {
	s := strings.ToUpper(path)
	s = strings.TrimPrefix(s, `\\.\`)
	const prefix = "PHYSICALDRIVE"
	if !strings.HasPrefix(s, prefix) {
		return -1, fmt.Errorf("not a physical drive path: %q", path)
	}
	var n int
	if _, err := fmt.Sscanf(s[len(prefix):], "%d", &n); err != nil {
		return -1, fmt.Errorf("parse drive number from %q: %w", path, err)
	}
	return n, nil
}

func volumesOnPhysicalDrive(driveNum int) ([]string, error) {
	buf := make([]uint16, 512)
	h, err := windows.FindFirstVolume(&buf[0], uint32(len(buf)))
	if err != nil {
		return nil, err
	}
	defer windows.FindVolumeClose(h)

	var vols []string
	for {
		vol := windows.UTF16ToString(buf)
		if on, _ := volumeOnDrive(vol, driveNum); on {
			vols = append(vols, vol)
		}
		if err := windows.FindNextVolume(h, &buf[0], uint32(len(buf))); err != nil {
			break
		}
	}
	return vols, nil
}

func volumeOnDrive(volGUID string, driveNum int) (bool, error) {
	openPath := strings.TrimRight(volGUID, `\`)
	openPtr, err := windows.UTF16PtrFromString(openPath)
	if err != nil {
		return false, err
	}
	h, err := windows.CreateFile(
		openPtr, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0,
	)
	if err != nil {
		return false, nil
	}
	defer windows.CloseHandle(h)

	out := make([]byte, 256)
	var returned uint32
	err = windows.DeviceIoControl(h, ioctlVolumeGetVolumeDiskExtents, nil, 0, &out[0], uint32(len(out)), &returned, nil)
	if err != nil || returned < 24 {
		return false, nil
	}
	diskN := int(out[8]) | int(out[9])<<8 | int(out[10])<<16 | int(out[11])<<24
	return diskN == driveNum, nil
}

func lockAndDismountVolumeHold(volGUID string) (windows.Handle, error) {
	openPath := strings.TrimRight(volGUID, `\`)
	openPtr, err := windows.UTF16PtrFromString(openPath)
	if err != nil {
		return 0, err
	}
	h, err := windows.CreateFile(
		openPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return 0, err
	}

	var returned uint32
	if err := windows.DeviceIoControl(h, fsctlLockVolume, nil, 0, nil, 0, &returned, nil); err != nil {
		// Try dismount anyway; some volumes lock only after dismount.
	}
	if err := windows.DeviceIoControl(h, fsctlDismountVolume, nil, 0, nil, 0, &returned, nil); err != nil {
		// Retry lock after dismount
		_ = windows.DeviceIoControl(h, fsctlLockVolume, nil, 0, nil, 0, &returned, nil)
	}
	return h, nil
}

// DISK_ATTRIBUTES structure for IOCTL_DISK_SET_DISK_ATTRIBUTES
type diskAttributes struct {
	Version        uint32
	Reserved1      uint32
	Attributes     uint64
	AttributesMask uint64
	Reserved2      [4]uint32
}

func setDiskOffline(devicePath string, offline bool) error {
	pathPtr, err := windows.UTF16PtrFromString(devicePath)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)

	attrs := diskAttributes{
		Version:        uint32(unsafe.Sizeof(diskAttributes{})),
		AttributesMask: diskAttributeOffline,
	}
	if offline {
		attrs.Attributes = diskAttributeOffline
	}

	var returned uint32
	return windows.DeviceIoControl(
		h, ioctlDiskSetDiskAttributes,
		(*byte)(unsafe.Pointer(&attrs)), uint32(unsafe.Sizeof(attrs)),
		nil, 0, &returned, nil,
	)
}
