//go:build linux

package disk

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func unmountDevicePlatform(devicePath string) error {
	base := filepath.Base(devicePath) // e.g. sdb, mmcblk0
	if base == "" || base == "." {
		return fmt.Errorf("invalid device path %q", devicePath)
	}

	mounts, err := mountedPathsForDevice(base)
	if err != nil {
		return err
	}
	if len(mounts) == 0 {
		return nil
	}

	var errs []string

	for i := len(mounts) - 1; i >= 0; i-- {
		mp := mounts[i]
		cmd := exec.Command("umount", mp)
		if out, err := cmd.CombinedOutput(); err != nil {
			// Try lazy unmount as fallback.
			cmd = exec.Command("umount", "-l", mp)
			if out2, err2 := cmd.CombinedOutput(); err2 != nil {
				errs = append(errs, fmt.Sprintf("%s: %s", mp, strings.TrimSpace(string(out2))))
				_ = out
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("unmount: %s", strings.Join(errs, "; "))
	}
	return nil
}

func mountedPathsForDevice(devBase string) ([]string, error) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		src := fields[0]

		base := filepath.Base(src)
		if base == devBase || strings.HasPrefix(base, devBase+"p") || strings.HasPrefix(base, devBase) {
			if base == devBase ||
				strings.HasPrefix(base, devBase+"p") || // mmcblk0p1
				(strings.HasPrefix(devBase, "sd") || strings.HasPrefix(devBase, "vd") || strings.HasPrefix(devBase, "nvme")) && strings.HasPrefix(base, devBase) {
				paths = append(paths, fields[1])
			}
		}
	}
	return paths, nil
}
