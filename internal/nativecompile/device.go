package nativecompile

// Device represents a connected Android device with its capabilities.
type Device struct {
	Serial   string
	APILevel int
	ABIs     []string
	Density  int
}

// DeviceConstraints describes the requirements a variant places on a device.
type DeviceConstraints struct {
	MinSDK       int
	RequiredABIs []string // at least one must be supported by the device
}

// FilterDevices returns the subset of devices compatible with the given
// constraints. A device matches if its API level is at least MinSDK and it
// supports at least one of the required ABIs. An empty RequiredABIs list
// means any ABI is acceptable.
func FilterDevices(devices []Device, c DeviceConstraints) []Device {
	var matched []Device
	for _, d := range devices {
		if !meetsMinSDK(d, c.MinSDK) {
			continue
		}
		if !meetsABI(d, c.RequiredABIs) {
			continue
		}
		matched = append(matched, d)
	}
	return matched
}

func meetsMinSDK(d Device, minSDK int) bool {
	if minSDK <= 0 {
		return true
	}
	return d.APILevel >= minSDK
}

func meetsABI(d Device, required []string) bool {
	if len(required) == 0 {
		return true
	}
	for _, r := range required {
		for _, a := range d.ABIs {
			if a == r {
				return true
			}
		}
	}
	return false
}
