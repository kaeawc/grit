package testsupport

import "strings"

type ADBInstallInvocation struct {
	DeviceSerial string
	APKPath      string
}

func ADBInstallArgs(deviceSerial, apkPath string) []string {
	args := []string{}
	if strings.TrimSpace(deviceSerial) != "" {
		args = append(args, "-s", deviceSerial)
	}
	return append(args, "install", "-r", apkPath)
}

func ParseADBInstallArgs(args []string) (ADBInstallInvocation, bool) {
	if len(args) < 3 {
		return ADBInstallInvocation{}, false
	}
	out := ADBInstallInvocation{}
	rest := append([]string(nil), args...)
	if len(rest) >= 3 && rest[0] == "-s" {
		out.DeviceSerial = rest[1]
		rest = rest[2:]
	}
	if len(rest) != 3 || rest[0] != "install" || rest[1] != "-r" || strings.TrimSpace(rest[2]) == "" {
		return ADBInstallInvocation{}, false
	}
	out.APKPath = rest[2]
	return out, true
}

func IsADBInstallInvocation(name string, args []string) bool {
	if name != "adb" {
		return false
	}
	_, ok := ParseADBInstallArgs(args)
	return ok
}
