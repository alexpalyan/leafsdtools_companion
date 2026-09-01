//go:build !windows

package privilege

import (
	"fmt"
	"net"
	"os"
)

func deviceSizeForHelper(f *os.File) (int64, error) {
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func helperDuplicateHandle(conn net.Conn, devicePath string, parentPID int, write bool) error {
	return fmt.Errorf("duplicate mode is only supported on Windows")
}
