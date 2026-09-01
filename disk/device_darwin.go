//go:build darwin

package disk

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	dkiocGetBlockSize  = 0x40046418
	dkiocGetBlockCount = 0x40086419
)

func listDevicesPlatform() ([]Device, error) {
	out, err := exec.Command("diskutil", "list", "-plist", "physical").Output()
	if err != nil {
		out, err = exec.Command("diskutil", "list", "-plist").Output()
		if err != nil {
			return nil, err
		}
	}

	diskIDs := extractPlistStringArray(string(out), "WholeDisks")
	if len(diskIDs) == 0 {
		textOut, textErr := exec.Command("diskutil", "list", "physical").Output()
		if textErr == nil {
			diskIDs = parseDiskIDsFromText(string(textOut))
		}
	}

	systemDisk := systemWholeDisk()

	var devices []Device
	for _, id := range diskIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}

		if id == systemDisk {
			continue
		}

		info, err := exec.Command("diskutil", "info", "-plist", id).Output()
		if err != nil {
			continue
		}
		text := string(info)

		virt := extractPlistString(text, "VirtualOrPhysical")
		if strings.EqualFold(virt, "Virtual") {
			continue
		}
		if extractPlistBool(text, "Virtual") {
			continue
		}

		name := extractPlistString(text, "MediaName")
		if name == "" {
			name = extractPlistString(text, "IORegistryEntryName")
		}
		if name == "" {
			name = extractPlistString(text, "DeviceIdentifier")
		}
		if name == "" {
			name = id
		}

		sizeStr := extractPlistString(text, "TotalSize")
		if sizeStr == "" {
			sizeStr = extractPlistString(text, "Size")
		}
		size, _ := strconv.ParseInt(sizeStr, 10, 64)
		if size > MaxDiskSize {
			continue
		}

		// Raw device better if available
		path := "/dev/r" + id
		if _, err := os.Stat(path); err != nil {
			path = "/dev/" + id
		}

		devices = append(devices, Device{
			Path: path,
			Name: name,
			Size: size,
		})
	}
	return devices, nil
}

func systemWholeDisk() string {
	out, err := exec.Command("diskutil", "info", "-plist", "/").Output()
	if err != nil {
		return ""
	}
	text := string(out)
	for _, key := range []string{"ParentWholeDisk", "WholeDisk", "DeviceIdentifier"} {
		if v := extractPlistString(text, key); v != "" && !strings.Contains(v, "s") {
			return v
		}
	}
	id := extractPlistString(text, "DeviceIdentifier")
	if i := strings.Index(id, "s"); i > 0 {
		return id[:i]
	}
	return id
}

func parseDiskIDsFromText(text string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)

		if !strings.HasPrefix(line, "/dev/disk") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		dev := strings.TrimPrefix(fields[0], "/dev/")

		if strings.Contains(dev, "s") {
			continue
		}
		if !seen[dev] {
			seen[dev] = true
			ids = append(ids, dev)
		}
	}
	return ids
}

func extractPlistString(xml, key string) string {
	marker := "<key>" + key + "</key>"
	idx := strings.Index(xml, marker)
	if idx == -1 {
		return ""
	}
	rest := xml[idx+len(marker):]
	rest = strings.TrimLeft(rest, " \t\r\n")

	if strings.HasPrefix(rest, "<string>") {
		start := len("<string>")
		end := strings.Index(rest[start:], "</string>")
		if end == -1 {
			return ""
		}
		return strings.TrimSpace(rest[start : start+end])
	}
	if strings.HasPrefix(rest, "<integer>") {
		start := len("<integer>")
		end := strings.Index(rest[start:], "</integer>")
		if end == -1 {
			return ""
		}
		return strings.TrimSpace(rest[start : start+end])
	}
	return ""
}

func extractPlistBool(xml, key string) bool {
	marker := "<key>" + key + "</key>"
	idx := strings.Index(xml, marker)
	if idx == -1 {
		return false
	}
	rest := strings.TrimLeft(xml[idx+len(marker):], " \t\r\n")
	return strings.HasPrefix(rest, "<true") || strings.HasPrefix(rest, "<true/>")
}

func extractPlistStringArray(xml, key string) []string {
	marker := "<key>" + key + "</key>"
	idx := strings.Index(xml, marker)
	if idx == -1 {
		return nil
	}
	rest := xml[idx+len(marker):]
	arrStart := strings.Index(rest, "<array>")
	if arrStart == -1 {
		return nil
	}

	arrEnd := strings.Index(rest[arrStart:], "</array>")
	if arrEnd == -1 {
		return nil
	}
	body := rest[arrStart+len("<array>") : arrStart+arrEnd]
	var result []string
	for _, part := range strings.Split(body, "<string>") {
		if end := strings.Index(part, "</string>"); end != -1 {
			s := strings.TrimSpace(part[:end])
			if s != "" {
				result = append(result, s)
			}
		}
	}
	return result
}

func deviceSizePlatform(f *os.File) (int64, error) {
	var blockSize uint32
	if _, _, errno := unix.Syscall(
		unix.SYS_IOCTL, f.Fd(), dkiocGetBlockSize, uintptr(unsafe.Pointer(&blockSize)),
	); errno != 0 {
		return 0, errno
	}

	var blockCount uint64
	if _, _, errno := unix.Syscall(
		unix.SYS_IOCTL, f.Fd(), dkiocGetBlockCount, uintptr(unsafe.Pointer(&blockCount)),
	); errno != 0 {
		return 0, errno
	}

	return int64(blockCount) * int64(blockSize), nil
}
