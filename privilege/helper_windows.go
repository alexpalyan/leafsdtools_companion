//go:build windows

package privilege

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const ioctlDiskGetLengthInfo = 0x0007405C

type getLengthInformation struct {
	Length int64
}

func deviceSizeForHelper(f *os.File) (int64, error) {
	handle := windows.Handle(f.Fd())
	var info getLengthInformation
	var bytesReturned uint32
	err := windows.DeviceIoControl(
		handle,
		ioctlDiskGetLengthInfo,
		nil, 0,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)),
		&bytesReturned,
		nil,
	)
	if err != nil {
		return 0, err
	}
	return info.Length, nil
}

func helperDuplicateHandle(conn net.Conn, devicePath string, parentPID int, write bool) error {
	pathPtr, err := windows.UTF16PtrFromString(devicePath)
	if err != nil {
		return err
	}
	access := uint32(windows.GENERIC_READ)
	if write {
		access |= windows.GENERIC_WRITE
	}
	h, err := windows.CreateFile(
		pathPtr,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return fmt.Errorf("CreateFile(%q): %w", devicePath, err)
	}
	defer windows.CloseHandle(h)

	var size int64
	{
		var info getLengthInformation
		var bytesReturned uint32
		if err := windows.DeviceIoControl(
			h,
			ioctlDiskGetLengthInfo,
			nil, 0,
			(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)),
			&bytesReturned,
			nil,
		); err == nil {
			size = info.Length
		}
	}

	parentProc, err := windows.OpenProcess(
		windows.PROCESS_DUP_HANDLE,
		false,
		uint32(parentPID),
	)
	if err != nil {
		return fmt.Errorf("OpenProcess(pid=%d): %w", parentPID, err)
	}
	defer windows.CloseHandle(parentProc)

	var targetHandle windows.Handle
	err = windows.DuplicateHandle(
		windows.CurrentProcess(),
		h,
		parentProc,
		&targetHandle,
		0,
		false,
		windows.DUPLICATE_SAME_ACCESS,
	)
	if err != nil {
		return fmt.Errorf("DuplicateHandle: %w", err)
	}

	var buf [16]byte
	binary.BigEndian.PutUint64(buf[0:8], uint64(size))
	binary.BigEndian.PutUint64(buf[8:16], uint64(targetHandle))
	if _, err := conn.Write(buf[:]); err != nil {
		return fmt.Errorf("writing handle to parent: %w", err)
	}
	return nil
}
