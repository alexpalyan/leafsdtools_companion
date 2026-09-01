//go:build linux

package disk

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

func listDevicesPlatform() ([]Device, error) {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil, err
	}

	var devices []Device
	for _, e := range entries {
		name := e.Name()

		if strings.HasPrefix(name, "loop") ||
			strings.HasPrefix(name, "ram") ||
			strings.HasPrefix(name, "zram") ||
			strings.HasPrefix(name, "dm-") {
			continue
		}

		sizeSectors := readSysUint(filepath.Join("/sys/block", name, "size"))
		size := sizeSectors * 512
		if size > MaxDiskSize {
			continue
		}
		vendor := strings.TrimSpace(readSysString(filepath.Join("/sys/block", name, "device", "vendor")))
		model := strings.TrimSpace(readSysString(filepath.Join("/sys/block", name, "device", "model")))
		label := strings.TrimSpace(vendor + " " + model)
		if label == "" {
			label = name
		}

		devices = append(devices, Device{
			Path: "/dev/" + name,
			Name: label,
			Size: size,
		})
	}
	return devices, nil
}

func readSysUint(path string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func readSysString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func deviceSizePlatform(f *os.File) (int64, error) {
	var size uint64
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		f.Fd(),
		unix.BLKGETSIZE64,
		uintptr(unsafe.Pointer(&size)),
	)
	if errno != 0 {
		return 0, errno
	}
	return int64(size), nil
}
