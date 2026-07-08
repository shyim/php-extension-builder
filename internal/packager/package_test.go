package packager

import (
	"testing"
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

	expected := "php_foo-1.2.3_php8.3-x86_64-linux-glibc-debug-zts.zip"
	if details.Filename() != expected {
		t.Errorf("expected %q, got %q", expected, details.Filename())
	}
}

func TestTrimsPhpVersionToMajorMinor(t *testing.T) {
	if PhpMajorMinor("8.3.10-whatever\n") != "8.3" {
		t.Errorf("expected %q, got %q", "8.3", PhpMajorMinor("8.3.10-whatever\n"))
	}
}
