//go:build windows

package disk

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	fsctlLockVolume                 = 0x00090018
	fsctlDismountVolume             = 0x00090020
	ioctlVolumeGetVolumeDiskExtents = 0x00560000
)

func unmountDevicePlatform(devicePath string) error {
	// typically \\.\PHYSICALDRIVE N
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
		if err := lockAndDismountVolume(vol); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", vol, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("dismount: %s", strings.Join(errs, "; "))
	}
	return nil
}

func physicalDriveNumber(path string) (int, error) {
	// \\.\PHYSICALDRIVE0  or  \\.\PhysicalDrive0
	s := strings.ToUpper(path)
	s = strings.TrimPrefix(s, `\\.\`)
	s = strings.TrimPrefix(s, `\\.\`)
	const prefix = "PHYSICALDRIVE"
	if !strings.HasPrefix(s, prefix) {
		return -1, fmt.Errorf("not a physical drive path: %q", path)
	}
	var n int
	_, err := fmt.Sscanf(s[len(prefix):], "%d", &n)
	if err != nil {
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
		err = windows.FindNextVolume(h, &buf[0], uint32(len(buf)))
		if err != nil {
			break
		}
	}
	return vols, nil
}

func volumeOnDrive(volGUID string, driveNum int) (bool, error) {
	path := volGUID
	if !strings.HasSuffix(path, `\`) {
		path += `\`
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}

	openPath := strings.TrimRight(volGUID, `\`)
	openPtr, err := windows.UTF16PtrFromString(openPath)
	if err != nil {
		return false, err
	}
	h, err := windows.CreateFile(
		openPtr,
		0, // no access needed
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		_ = pathPtr
		return false, nil // not a mountable volume
	}
	defer windows.CloseHandle(h)

	out := make([]byte, 256)
	var returned uint32
	err = windows.DeviceIoControl(
		h,
		ioctlVolumeGetVolumeDiskExtents,
		nil, 0,
		&out[0], uint32(len(out)),
		&returned,
		nil,
	)
	if err != nil || returned < 8 {
		return false, nil
	}
	n := int(out[0]) | int(out[1])<<8 | int(out[2])<<16 | int(out[3])<<24
	if n < 1 || returned < 24 {
		return false, nil
	}

	diskN := int(out[8]) | int(out[9])<<8 | int(out[10])<<16 | int(out[11])<<24
	return diskN == driveNum, nil
}

func lockAndDismountVolume(volGUID string) error {
	openPath := strings.TrimRight(volGUID, `\`)
	openPtr, err := windows.UTF16PtrFromString(openPath)
	if err != nil {
		return err
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
		return err
	}
	defer windows.CloseHandle(h)

	var returned uint32
	// Lock
	if err := windows.DeviceIoControl(h, fsctlLockVolume, nil, 0, nil, 0, &returned, nil); err != nil {
		// Still try dismount
	}
	if err := windows.DeviceIoControl(h, fsctlDismountVolume, nil, 0, nil, 0, &returned, nil); err != nil {
		return err
	}
	_ = unsafe.Sizeof(returned)
	_ = filepath.Separator
	return nil
}
