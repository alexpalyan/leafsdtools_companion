package disk

// VolumeHold represents held exclusive locks on a device's volumes.
// Close must be called when the imaging operation finishes
type VolumeHold interface {
	Close() error
}

// HoldDeviceVolumes unmounts (and on Windows, locks) every volume on the
// disk so the OS cannot auto-mount partitions while a raw write is progressing
func HoldDeviceVolumes(devicePath string) (VolumeHold, error) {
	return holdDeviceVolumesPlatform(devicePath)
}
