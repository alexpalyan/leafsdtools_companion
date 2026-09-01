//go:build !windows

package disk

type nopHold struct{}

func (nopHold) Close() error { return nil }

func holdDeviceVolumesPlatform(devicePath string) (VolumeHold, error) {
	// No need to "hold" on Unix
	if err := unmountDevicePlatform(devicePath); err != nil {
		return nopHold{}, err
	}
	return nopHold{}, nil
}
