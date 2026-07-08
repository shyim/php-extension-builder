package packager

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"testing"
)

func TestCreatesDebianPackageFilename(t *testing.T) {
	d := DebPackageDetails{
		ExtensionName:  "foo",
		PackageVersion: "1.2.3",
		PhpMajorMinor:  "8.3",
		Arch:           "x86_64",
		ExtensionDir:   "/usr/local/lib/php/extensions/no-debug-non-zts-20230831",
	}

	name, err := d.Filename()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "php8.3-foo_1.2.3_amd64.deb" {
		t.Errorf("expected php8.3-foo_1.2.3_amd64.deb, got %q", name)
	}
}

func TestNormalizesExtensionNameForDebianPackageName(t *testing.T) {
	if debianPackageComponent("Test_Ext") != "test-ext" {
		t.Errorf("expected test-ext, got %q", debianPackageComponent("Test_Ext"))
	}
}

func TestMapsDebianArchitectures(t *testing.T) {
	if arch, _ := debianArchitecture("x86_64"); arch != "amd64" {
		t.Errorf("expected amd64, got %q", arch)
	}
	if arch, _ := debianArchitecture("arm64"); arch != "arm64" {
		t.Errorf("expected arm64, got %q", arch)
	}
	if arch, _ := debianArchitecture("x86"); arch != "i386" {
		t.Errorf("expected i386, got %q", arch)
	}
	if _, err := debianArchitecture("sparc"); err == nil {
		t.Error("expected error for sparc, got nil")
	}
}

func TestExtractsPhpApiFromExtensionDir(t *testing.T) {
	api, err := phpApiFromExtensionDir("/usr/local/lib/php/extensions/no-debug-non-zts-20230831")
	if err != nil || api != "20230831" {
		t.Errorf("expected 20230831, got %q (err: %v)", api, err)
	}
	api, err = phpApiFromExtensionDir("/usr/lib/php/20230831")
	if err != nil || api != "20230831" {
		t.Errorf("expected 20230831, got %q (err: %v)", api, err)
	}
	_, err = phpApiFromExtensionDir("/usr/lib/php/extensions/debug")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestCreatesMaintainerScripts(t *testing.T) {
	if !strings.Contains(postinstScript("foo", "8.3"), "phpenmod -v '8.3' 'foo' || true") {
		t.Error("missing phpenmod call")
	}
	if !strings.Contains(prermScript("foo", "8.3"), "phpdismod -v '8.3' 'foo' || true") {
		t.Error("missing phpdismod call")
	}
}

func TestCreatesDebArchiveMembersAndPayloads(t *testing.T) {
	d := DebPackageDetails{
		ExtensionName:  "foo",
		PackageVersion: "1.2.3",
		PhpMajorMinor:  "8.3",
		Arch:           "x86_64",
		ExtensionDir:   "/usr/local/lib/php/extensions/no-debug-non-zts-20230831",
	}

	tmpSo, err := os.CreateTemp("", "fake-so-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpSo.Name())
	defer tmpSo.Close()
	if _, err := tmpSo.Write([]byte("fake-so")); err != nil {
		t.Fatalf("failed to write fake-so: %v", err)
	}

	tmpDeb, err := os.CreateTemp("", "output-deb-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpDeb.Name())
	tmpDeb.Close() // Close so CreateDeb can write to it

	err = CreateDeb(tmpSo.Name(), tmpDeb.Name(), d)
	if err != nil {
		t.Fatalf("failed to build package: %v", err)
	}

	pkg, err := os.ReadFile(tmpDeb.Name())
	if err != nil {
		t.Fatalf("failed to read generated deb package: %v", err)
	}

	members, err := parseAr(pkg)
	if err != nil {
		t.Fatalf("failed to parse ar: %v", err)
	}

	if string(members["debian-binary"]) != "2.0\n" {
		t.Errorf("expected 2.0\\n, got %q", string(members["debian-binary"]))
	}

	controlFiles, err := tarFiles(members["control.tar.gz"])
	if err != nil {
		t.Fatalf("failed to parse control.tar.gz: %v", err)
	}

	if !strings.Contains(string(controlFiles["control"]), "Package: php8.3-foo") {
		t.Error("control file does not contain Package: php8.3-foo")
	}
	if _, ok := controlFiles["postinst"]; !ok {
		t.Error("missing postinst script")
	}
	if _, ok := controlFiles["prerm"]; !ok {
		t.Error("missing prerm script")
	}

	dataFiles, err := tarFiles(members["data.tar.gz"])
	if err != nil {
		t.Fatalf("failed to parse data.tar.gz: %v", err)
	}

	if string(dataFiles["usr/lib/php/20230831/foo.so"]) != "fake-so" {
		t.Errorf("expected fake-so, got %q", string(dataFiles["usr/lib/php/20230831/foo.so"]))
	}

	if string(dataFiles["etc/php/8.3/mods-available/foo.ini"]) != "extension=foo.so\n" {
		t.Errorf("expected extension=foo.so\\n, got %q", string(dataFiles["etc/php/8.3/mods-available/foo.ini"]))
	}
}

func parseAr(data []byte) (map[string][]byte, error) {
	if !bytes.HasPrefix(data, []byte("!<arch>\n")) {
		return nil, fmt.Errorf("missing ar header")
	}

	members := make(map[string][]byte)
	offset := 8
	for offset < len(data) {
		if offset+60 > len(data) {
			return nil, fmt.Errorf("truncated ar header")
		}
		header := data[offset : offset+60]
		name := strings.TrimRight(strings.TrimSpace(string(header[0:16])), "/")
		var size int
		if _, err := fmt.Sscanf(string(header[48:58]), "%d", &size); err != nil {
			return nil, fmt.Errorf("failed to parse ar member size: %w", err)
		}
		offset += 60

		if offset+size > len(data) {
			return nil, fmt.Errorf("truncated ar member")
		}
		members[name] = data[offset : offset+size]
		offset += size
		if size%2 != 0 {
			offset++
		}
	}
	return members, nil
}

func tarFiles(gzipTar []byte) (map[string][]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(gzipTar))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	files := make(map[string][]byte)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		var content bytes.Buffer
		if _, err := io.Copy(&content, tr); err != nil {
			return nil, err
		}
		// Clean and normalize name to match both "./name" and "name"
		name := path.Clean(header.Name)
		name = strings.TrimPrefix(name, ".")
		name = strings.TrimPrefix(name, "/")
		files[name] = content.Bytes()
	}
	return files, nil
}
