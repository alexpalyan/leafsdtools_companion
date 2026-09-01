package privilege

import "io"

func IsElevated() bool {
	return isElevatedPlatform()
}

// OpenElevatedDeviceRead opens devicePath for reading only.
func OpenElevatedDeviceRead(devicePath string) (io.ReadCloser, int64, error) {
	return openElevatedDevicePlatform(devicePath, false)
}

// OpenElevatedDeviceReadWrite opens devicePath for reading and writing.
func OpenElevatedDeviceReadWrite(devicePath string) (io.ReadWriteCloser, int64, error) {
	return openElevatedDevicePlatform(devicePath, true)
}
