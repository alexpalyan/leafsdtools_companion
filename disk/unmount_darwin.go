//go:build darwin

package disk

import (
	"fmt"
	"os/exec"
	"strings"
)

func unmountDevicePlatform(devicePath string) error {
	id := wholeDiskID(devicePath)
	if id == "" {
		return fmt.Errorf("could not determine whole-disk id for %q", devicePath)
	}

	mounted, err := diskHasMountedVolumes(id)
	if err != nil {
		// If we can't tell, still try to unmount (best effort).
		mounted = true
	}
	if !mounted {
		return nil
	}

	cmd := exec.Command("diskutil", "unmountDisk", id)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if alreadyUnmountedMessage(msg) {
		return nil
	}

	cmd = exec.Command("diskutil", "unmountDisk", "force", id)
	out2, err2 := cmd.CombinedOutput()
	if err2 != nil {
		msg2 := strings.TrimSpace(string(out2))
		if alreadyUnmountedMessage(msg2) {
			return nil
		}
		if msg2 == "" {
			msg2 = msg
		}
		if msg2 == "" {
			msg2 = err2.Error()
		}
		return fmt.Errorf("unmountDisk %s: %s", id, msg2)
	}
	return nil
}

func diskHasMountedVolumes(id string) (bool, error) {
	out, err := exec.Command("mount").Output()
	if err != nil {
		return false, err
	}

	prefix := "/dev/" + id
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix+" ") || strings.HasPrefix(line, prefix+"s") {
			return true, nil
		}
	}
	return false, nil
}

func alreadyUnmountedMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "not mounted") ||
		strings.Contains(lower, "already unmounted") ||
		strings.Contains(lower, "was successful") && strings.Contains(lower, "unmount")
}

func wholeDiskID(path string) string {
	s := path
	s = strings.TrimPrefix(s, "/dev/")
	s = strings.TrimPrefix(s, "r") // rdiskN → diskN
	if i := strings.Index(s, "s"); i > 0 {
		rest := s[i+1:]
		if rest != "" && rest[0] >= '0' && rest[0] <= '9' {
			s = s[:i]
		}
	}
	if !strings.HasPrefix(s, "disk") {
		return ""
	}
	return s
}
