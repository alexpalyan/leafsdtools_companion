//go:build !linux

package disk

// alignedBuffer: O_DIRECT alignment is only relevant on Linux
func alignedBuffer(size int) []byte {
	return make([]byte, size)
}

func clearDirectIO(fd uintptr) error { return nil }

func dropCachedPages(fd uintptr, length int64) {}
