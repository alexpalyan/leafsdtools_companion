//go:build linux

package privilege

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

func isElevatedPlatform() bool {
	return os.Geteuid() == 0
}

func openElevatedDevicePlatform(devicePath string, write bool) (io.ReadWriteCloser, int64, error) {
	mode := "r"
	if write {
		mode = "rw"
	}
	if rc, size, err := openViaUDisks2(devicePath, mode); err == nil {
		return rc, size, nil
	}
	if write {
		return nil, 0, fmt.Errorf("cannot open %q for write: udisks2 unavailable", devicePath)
	}
	rc, size, err := openElevatedDeviceReadViaHelperStream(devicePath)
	if err != nil {
		return nil, 0, err
	}
	return readOnlyAsRWC{rc}, size, nil
}

// readOnlyAsRWC adapts a read-only closer to ReadWriteCloser (Write always fails)
type readOnlyAsRWC struct{ io.ReadCloser }

func (r readOnlyAsRWC) Write([]byte) (int, error) {
	return 0, fmt.Errorf("device opened read-only")
}

func relaunchElevatedHelperPlatform(exe string, args []string) error {
	helpers := []string{"pkexec", "gksudo", "kdesu", "lxqt-sudo"}
	var lastErr error
	for _, helper := range helpers {
		path, err := exec.LookPath(helper)
		if err != nil {
			continue
		}
		cmdArgs := append([]string{exe}, args...)
		if startErr := exec.Command(path, cmdArgs...).Start(); startErr == nil {
			return nil
		} else {
			lastErr = startErr
		}
	}
	if lastErr != nil {
		return fmt.Errorf("no privilege-escalation helper succeeded: %w", lastErr)
	}
	return fmt.Errorf("no privilege-escalation helper found (tried pkexec, gksudo, kdesu, lxqt-sudo)")
}
