package packager

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreatesPiePackageFilename(t *testing.T) {
	details := PackageDetails{
		ExtensionName:  "foo",
		PackageVersion: "1.2.3",
		PhpMajorMinor:  "8.3",
		Arch:           "x86_64",
		Os:             "linux",
		Libc:           "glibc",
		DebugSuffix:    "-debug",
		ZtsSuffix:      "-zts",
	}

	assert.Equal(t, "php_foo-1.2.3_php8.3-x86_64-linux-glibc-debug-zts.zip", details.Filename())
}

func TestTrimsPhpVersionToMajorMinor(t *testing.T) {
	assert.Equal(t, "8.3", PhpMajorMinor("8.3.10-whatever\n"))
}
