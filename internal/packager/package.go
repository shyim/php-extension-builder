package packager

import (
	"fmt"
	"strings"
)

type PackageDetails struct {
	ExtensionName  string
	PackageVersion string
	PhpMajorMinor  string
	Arch           string
	Os             string
	Libc           string
	DebugSuffix    string
	ZtsSuffix      string
}

func (p PackageDetails) Filename() string {
	return fmt.Sprintf(
		"php_%s-%s_php%s-%s-%s-%s%s%s.zip",
		p.ExtensionName,
		p.PackageVersion,
		p.PhpMajorMinor,
		p.Arch,
		p.Os,
		p.Libc,
		p.DebugSuffix,
		p.ZtsSuffix,
	)
}

func PhpMajorMinor(version string) string {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) > 2 {
		return strings.Join(parts[:2], ".")
	}
	return strings.Join(parts, ".")
}
