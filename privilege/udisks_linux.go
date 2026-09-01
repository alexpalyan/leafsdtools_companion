//go:build linux

package privilege

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
)

func openViaUDisks2(devicePath, mode string) (io.ReadWriteCloser, int64, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, 0, fmt.Errorf("connecting to D-Bus system bus: %w", err)
	}
	defer conn.Close()

	blockPath := udisks2BlockObjectPath(devicePath)
	obj := conn.Object("org.freedesktop.UDisks2", blockPath)

	var fd dbus.UnixFD
	options := map[string]dbus.Variant{}
	call := obj.Call("org.freedesktop.UDisks2.Block.OpenDevice", 0, mode, options)
	if call.Err != nil {
		return nil, 0, fmt.Errorf("udisks2 OpenDevice(%s, %s): %w", blockPath, mode, call.Err)
	}
	if err := call.Store(&fd); err != nil {
		return nil, 0, fmt.Errorf("reading udisks2 OpenDevice reply: %w", err)
	}

	f := os.NewFile(uintptr(fd), devicePath)
	var size int64
	if fi, statErr := f.Stat(); statErr == nil {
		size = fi.Size()
	}
	return f, size, nil
}

func udisks2BlockObjectPath(devicePath string) dbus.ObjectPath {
	name := devicePath
	if idx := strings.LastIndex(devicePath, "/"); idx != -1 {
		name = devicePath[idx+1:]
	}
	return dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/" + name)
}
