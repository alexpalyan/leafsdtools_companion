//go:build linux

package disk

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

// alignedBuffer returns a byte slice of length size whose backing array
// starts at a directIOAlign-aligned address, as required for O_DIRECT
func alignedBuffer(size int) []byte {
	buf := make([]byte, size+directIOAlign)
	addr := uintptr(unsafe.Pointer(&buf[0]))
	off := int(addr % directIOAlign)
	if off != 0 {
		off = directIOAlign - off
	}
	return buf[off : off+size : off+size]
}

// clearDirectIO drops O_DIRECT from fd.
func clearDirectIO(fd uintptr) error {
	flags, err := unix.FcntlInt(fd, unix.F_GETFL, 0)
	if err != nil {
		return err
	}
	_, err = unix.FcntlInt(fd, unix.F_SETFL, flags&^unix.O_DIRECT)
	return err
}

// dropCachedPages advises the kernel to drop any cached pages for fd over 0
func dropCachedPages(fd uintptr, length int64) {
	_ = unix.Fadvise(int(fd), 0, length, unix.FADV_DONTNEED)
}
