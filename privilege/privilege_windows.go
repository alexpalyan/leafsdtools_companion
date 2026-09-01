//go:build windows

package privilege

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

func isElevatedPlatform() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

func openElevatedDevicePlatform(devicePath string, write bool) (io.ReadWriteCloser, int64, error) {
	mode := "ro"
	if write {
		mode = "rw"
	}
	conn, err := startElevatedHelperConn("duplicate", devicePath, strconv.Itoa(os.Getpid()), mode)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()

	var buf [16]byte
	if _, err := io.ReadFull(conn, buf[:]); err != nil {
		return nil, 0, fmt.Errorf("elevated helper did not hand over a device handle: %w", err)
	}

	size := int64(binary.BigEndian.Uint64(buf[0:8]))
	handleVal := binary.BigEndian.Uint64(buf[8:16])
	if handleVal == 0 {
		return nil, 0, fmt.Errorf("elevated helper failed to open %q", devicePath)
	}

	f := os.NewFile(uintptr(handleVal), devicePath)
	if f == nil {
		return nil, 0, fmt.Errorf("could not wrap duplicated handle for %q", devicePath)
	}
	return f, size, nil
}

func relaunchElevatedHelperPlatform(exe string, args []string) error {
	exePtr, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	verbPtr, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}

	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
	}
	argPtr, err := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		return err
	}

	var cwdPtr *uint16
	if cwd, err := os.Getwd(); err == nil {
		cwdPtr, _ = windows.UTF16PtrFromString(cwd)
	}

	return windows.ShellExecute(0, verbPtr, exePtr, argPtr, cwdPtr, windows.SW_HIDE)
}
