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
	ioctlStorageGetDeviceNumber     = 0x002D1080
	storageDevicePropertyID         = 0
	propertyStandardQuery           = 0

	diskExtentsHeaderSize = 8
	diskExtentSize        = 24

	// for unmount
	fsctlLockVolume     = 0x00090018
	fsctlDismountVolume = 0x00090020
)

// GUID_DEVINTERFACE_DISK = {53F56307-B6BF-11D0-94F2-00A0C91EFB8B}
var guidDevInterfaceDisk = windows.GUID{
	Data1: 0x53F56307,
	Data2: 0xB6BF,
	Data3: 0x11D0,
	Data4: [8]byte{0x94, 0xF2, 0x00, 0xA0, 0xC9, 0x1E, 0xFB, 0x8B},
}

// GUID_DEVCLASS_DISKDRIVE = {4d36e967-e325-11ce-bfc1-08002be10318}
var guidDevClassDiskDrive = windows.GUID{
	Data1: 0x4d36e967,
	Data2: 0xe325,
	Data3: 0x11ce,
	Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18},
}

type getLengthInformation struct {
	Length int64
}

type storageDeviceNumber struct {
	DeviceType      uint32
	DeviceNumber    uint32
	PartitionNumber uint32
}

func deviceSizePlatform(f *os.File) (int64, error) {
	handle := windows.Handle(f.Fd())
	var info getLengthInformation
	var bytesReturned uint32
	err := windows.DeviceIoControl(
		handle, ioctlDiskGetLengthInfo,
		nil, 0,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)),
		&bytesReturned, nil,
	)
	if err != nil {
		return 0, err
	}
	return info.Length, nil
}

func listDevicesPlatform() ([]Device, error) {
	if devices, err := listDevicesViaSetupAPI(); err == nil && len(devices) > 0 {
		return devices, nil
	}
	return listDevicesViaVolumes()
}

func listDevicesViaSetupAPI() ([]Device, error) {
	devInfo, err := windows.SetupDiGetClassDevsEx(
		&guidDevClassDiskDrive, "", 0, windows.DIGCF_PRESENT, 0, "",
	)
	if err != nil {
		return nil, fmt.Errorf("SetupDiGetClassDevsEx: %w", err)
	}
	defer devInfo.Close()

	systemDisk := systemDiskIndex()
	letterMap := driveLettersByDisk()
	seen := map[uint32]bool{}
	var devices []Device

	for i := 0; ; i++ {
		devData, err := devInfo.EnumDeviceInfo(i)
		if err != nil {
			break
		}

		instanceID, err := devInfo.DeviceInstanceID(devData)
		if err != nil {
			continue
		}

		interfaces, err := windows.CM_Get_Device_Interface_List(
			instanceID, &guidDevInterfaceDisk, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT,
		)
		if err != nil || len(interfaces) == 0 {
			continue
		}
		devPath := interfaces[0]

		diskNum, size, model, probeErr := probeDiskInterface(devPath)
		if probeErr != nil {
			friendly := ""
			if prop, pErr := devInfo.DeviceRegistryProperty(devData, windows.SPDRP_FRIENDLYNAME); pErr == nil {
				if s, ok := prop.(string); ok {
					friendly = s
				}
			}
			if friendly == "" {
				if prop, pErr := devInfo.DeviceRegistryProperty(devData, windows.SPDRP_DEVICEDESC); pErr == nil {
					if s, ok := prop.(string); ok {
						friendly = s
					}
				}
			}
			if friendly == "" {
				friendly = "Disk"
			}
			devices = append(devices, Device{Path: devPath, Name: friendly, Size: 0})
			continue
		}

		if seen[diskNum] || int(diskNum) == systemDisk {
			continue
		}
		seen[diskNum] = true

		if model == "" {
			if prop, pErr := devInfo.DeviceRegistryProperty(devData, windows.SPDRP_FRIENDLYNAME); pErr == nil {
				if s, ok := prop.(string); ok {
					model = s
				}
			}
		}
		if model == "" {
			model = fmt.Sprintf("PhysicalDrive%d", diskNum)
		}

		name := model
		if letters := letterMap[int(diskNum)]; len(letters) > 0 {
			name = fmt.Sprintf("%s (%s)", model, strings.Join(letters, ", "))
		}

		devices = append(devices, Device{
			Path: fmt.Sprintf(`\\.\PhysicalDrive%d`, diskNum),
			Name: name,
			Size: size,
		})
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("SetupAPI found no disks")
	}
	return devices, nil
}

func probeDiskInterface(devicePath string) (diskNum uint32, size int64, model string, err error) {
	pathPtr, err := windows.UTF16PtrFromString(devicePath)
	if err != nil {
		return 0, 0, "", err
	}
	h, err := windows.CreateFile(
		pathPtr, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0,
	)
	if err != nil {
		return 0, 0, "", err
	}
	defer windows.CloseHandle(h)

	var sdn storageDeviceNumber
	var returned uint32
	if err := windows.DeviceIoControl(
		h, ioctlStorageGetDeviceNumber,
		nil, 0,
		(*byte)(unsafe.Pointer(&sdn)), uint32(unsafe.Sizeof(sdn)),
		&returned, nil,
	); err != nil {
		return 0, 0, "", err
	}
	diskNum = sdn.DeviceNumber

	var info getLengthInformation
	if err := windows.DeviceIoControl(
		h, ioctlDiskGetLengthInfo,
		nil, 0,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)),
		&returned, nil,
	); err == nil {
		size = info.Length
	}

	vendor, product, _ := queryStorageModel(h)
	model = strings.TrimSpace(vendor + " " + product)
	return diskNum, size, model, nil
}

func listDevicesViaVolumes() ([]Device, error) {
	letterMap := driveLettersByDisk()
	systemDisk := systemDiskIndex()
	seen := map[int]bool{}
	var devices []Device

	for diskNum, letters := range letterMap {
		if seen[diskNum] || diskNum == systemDisk {
			continue
		}
		seen[diskNum] = true
		name := fmt.Sprintf("PhysicalDrive%d", diskNum)
		if len(letters) > 0 {
			name = fmt.Sprintf("PhysicalDrive%d (%s)", diskNum, strings.Join(letters, ", "))
		}
		devices = append(devices, Device{
			Path: fmt.Sprintf(`\\.\PhysicalDrive%d`, diskNum),
			Name: name,
			Size: 0,
		})
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no disks found")
	}
	return devices, nil
}

func systemDiskIndex() int {
	winDir, err := windows.GetWindowsDirectory()
	if err != nil || len(winDir) < 3 {
		return 0
	}
	root := winDir[:2] + `\`
	volName := make([]uint16, windows.MAX_PATH)
	rootPtr, _ := windows.UTF16PtrFromString(root)
	if err := windows.GetVolumeNameForVolumeMountPoint(rootPtr, &volName[0], uint32(len(volName))); err != nil {
		return 0
	}
	openPath := strings.TrimSuffix(windows.UTF16ToString(volName), `\`)
	pathPtr, err := windows.UTF16PtrFromString(openPath)
	if err != nil {
		return 0
	}
	h, err := windows.CreateFile(pathPtr, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)

	out := make([]byte, 256)
	var returned uint32
	if err := windows.DeviceIoControl(h, ioctlVolumeGetVolumeDiskExtents, nil, 0, &out[0], uint32(len(out)), &returned, nil); err != nil || returned < 12 {
		return 0
	}
	return int(binary.LittleEndian.Uint32(out[8:12]))
}

func queryStorageModel(handle windows.Handle) (vendor, product string, err error) {
	in := make([]byte, 12)
	binary.LittleEndian.PutUint32(in[0:4], storageDevicePropertyID)
	binary.LittleEndian.PutUint32(in[4:8], propertyStandardQuery)
	out := make([]byte, 4096)
	var bytesReturned uint32
	if err := windows.DeviceIoControl(handle, ioctlStorageQueryProperty, &in[0], uint32(len(in)), &out[0], uint32(len(out)), &bytesReturned, nil); err != nil {
		return "", "", err
	}
	if bytesReturned < 36 {
		return "", "", fmt.Errorf("short descriptor")
	}
	vendor = cStringAt(out[:bytesReturned], binary.LittleEndian.Uint32(out[12:16]))
	product = cStringAt(out[:bytesReturned], binary.LittleEndian.Uint32(out[16:20]))
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
		addDriveLettersForVolume(windows.UTF16ToString(volNameBuf), result)
		if err := windows.FindNextVolume(handle, &volNameBuf[0], uint32(len(volNameBuf))); err != nil {
			break
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
	h, err := windows.CreateFile(pathPtr, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)

	out := make([]byte, 1024)
	var bytesReturned uint32
	if err := windows.DeviceIoControl(h, ioctlVolumeGetVolumeDiskExtents, nil, 0, &out[0], uint32(len(out)), &bytesReturned, nil); err != nil || bytesReturned < diskExtentsHeaderSize {
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
	if err := windows.GetVolumePathNamesForVolumeName(volPtr, &buf[0], uint32(len(buf)), &returnLen); err != nil {
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
