//go:build windows

package disk

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// (from <winioctl.h>)
const (
	ioctlDiskGetLengthInfo          = 0x0007405C
	ioctlStorageQueryProperty       = 0x002D1400
	ioctlVolumeGetVolumeDiskExtents = 0x00560000
	storageDevicePropertyID         = 0
	propertyStandardQuery           = 0

	diskExtentsHeaderSize = 8
	diskExtentSize        = 24
)

type getLengthInformation struct {
	Length int64
}

func deviceSizePlatform(f *os.File) (int64, error) {
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

func queryStorageModel(handle windows.Handle) (vendor, product string, err error) {
	in := make([]byte, 12) // sizeof(STORAGE_PROPERTY_QUERY)
	binary.LittleEndian.PutUint32(in[0:4], storageDevicePropertyID)
	binary.LittleEndian.PutUint32(in[4:8], propertyStandardQuery)

	out := make([]byte, 4096)
	var bytesReturned uint32

	ioErr := windows.DeviceIoControl(
		handle,
		ioctlStorageQueryProperty,
		&in[0], uint32(len(in)),
		&out[0], uint32(len(out)),
		&bytesReturned,
		nil,
	)
	if ioErr != nil {
		return "", "", ioErr
	}
	if bytesReturned < 36 {
		return "", "", fmt.Errorf("short STORAGE_DEVICE_DESCRIPTOR response (%d bytes)", bytesReturned)
	}

	vendorOff := binary.LittleEndian.Uint32(out[12:16])
	productOff := binary.LittleEndian.Uint32(out[16:20])

	vendor = cStringAt(out[:bytesReturned], vendorOff)
	product = cStringAt(out[:bytesReturned], productOff)
	return vendor, product, nil
}

func cStringAt(buf []byte, offset uint32) string {
	if offset == 0 || int(offset) >= len(buf) {
		return ""
	}
	end := int(offset)
	for end < len(buf) && buf[end] != 0 {
		end++
	}
	return strings.TrimSpace(string(buf[offset:end]))
}

func driveLettersByDisk() map[int][]string {
	result := map[int][]string{}

	volNameBuf := make([]uint16, windows.MAX_PATH)
	handle, err := windows.FindFirstVolume(&volNameBuf[0], uint32(len(volNameBuf)))
	if err != nil {
		return result
	}
	defer windows.FindVolumeClose(handle)

	for {
		volumeGUIDPath := windows.UTF16ToString(volNameBuf)
		addDriveLettersForVolume(volumeGUIDPath, result)

		err = windows.FindNextVolume(handle, &volNameBuf[0], uint32(len(volNameBuf)))
		if err != nil {
			break // ERROR_NO_MORE_FILES once enumeration is exhausted
		}
	}
	return result
}

func addDriveLettersForVolume(volumeGUIDPath string, result map[int][]string) {
	openPath := strings.TrimSuffix(volumeGUIDPath, `\`)
	pathPtr, err := windows.UTF16PtrFromString(openPath)
	if err != nil {
		return
	}

	h, err := windows.CreateFile(
		pathPtr,
		0, // no read/write access needed
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)

	out := make([]byte, 1024)
	var bytesReturned uint32
	ioErr := windows.DeviceIoControl(
		h, ioctlVolumeGetVolumeDiskExtents,
		nil, 0,
		&out[0], uint32(len(out)),
		&bytesReturned, nil,
	)
	if ioErr != nil || bytesReturned < diskExtentsHeaderSize {
		return
	}

	numExtents := binary.LittleEndian.Uint32(out[0:4])

	letters := driveLettersForVolume(volumeGUIDPath)
	if len(letters) == 0 {
		return
	}

	for i := uint32(0); i < numExtents; i++ {
		base := diskExtentsHeaderSize + i*diskExtentSize
		if int(base+4) > len(out) {
			break
		}
		diskNumber := int(binary.LittleEndian.Uint32(out[base : base+4]))
		result[diskNumber] = append(result[diskNumber], letters...)
	}
}

func driveLettersForVolume(volumeGUIDPath string) []string {
	volPtr, err := windows.UTF16PtrFromString(volumeGUIDPath)
	if err != nil {
		return nil
	}

	var returnLen uint32
	buf := make([]uint16, 256)
	err = windows.GetVolumePathNamesForVolumeName(volPtr, &buf[0], uint32(len(buf)), &returnLen)
	if err != nil {
		return nil
	}

	var letters []string
	for _, s := range splitDoubleNullUTF16(buf) {
		s = strings.TrimSuffix(s, `\`)
		if len(s) == 2 && s[1] == ':' {
			letters = append(letters, s)
		}
	}
	return letters
}

func splitDoubleNullUTF16(buf []uint16) []string {
	var result []string
	start := 0
	for i, c := range buf {
		if c == 0 {
			if i > start {
				result = append(result, windows.UTF16ToString(buf[start:i]))
			}
			start = i + 1
			if i+1 < len(buf) && buf[i+1] == 0 {
				break
			}
		}
	}
	return result
}

func listDevicesPlatform() ([]Device, error) {
	letterMap := driveLettersByDisk()

	var devices []Device
	for i := 0; i < 32; i++ {
		path := fmt.Sprintf(`\\.\PhysicalDrive%d`, i)
		f, err := os.Open(path)
		if err != nil {
			continue
		}

		size, _ := deviceSizePlatform(f)
		if size > MaxDiskSize {
			continue
		}
		vendor, product, _ := queryStorageModel(windows.Handle(f.Fd()))
		f.Close()

		model := strings.TrimSpace(vendor + " " + product)
		if model == "" {
			model = fmt.Sprintf("PhysicalDrive%d", i)
		}

		name := model
		if letters := letterMap[i]; len(letters) > 0 {
			name = fmt.Sprintf("%s (%s)", model, strings.Join(letters, ", "))
		}

		devices = append(devices, Device{
			Path: path,
			Name: name,
			Size: size,
		})
	}
	return devices, nil
}
