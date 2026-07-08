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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.NoError(t, err)
	assert.Equal(t, "php8.3-foo_1.2.3_amd64.deb", name)
}

func TestNormalizesExtensionNameForDebianPackageName(t *testing.T) {
	assert.Equal(t, "test-ext", debianPackageComponent("Test_Ext"))
}

func TestMapsDebianArchitectures(t *testing.T) {
	arch, err := debianArchitecture("x86_64")
	assert.NoError(t, err)
	assert.Equal(t, "amd64", arch)

	arch, err = debianArchitecture("arm64")
	assert.NoError(t, err)
	assert.Equal(t, "arm64", arch)

	arch, err = debianArchitecture("x86")
	assert.NoError(t, err)
	assert.Equal(t, "i386", arch)

	_, err = debianArchitecture("sparc")
	assert.Error(t, err)
}

func TestExtractsPhpApiFromExtensionDir(t *testing.T) {
	api, err := phpApiFromExtensionDir("/usr/local/lib/php/extensions/no-debug-non-zts-20230831")
	assert.NoError(t, err)
	assert.Equal(t, "20230831", api)

	api, err = phpApiFromExtensionDir("/usr/lib/php/20230831")
	assert.NoError(t, err)
	assert.Equal(t, "20230831", api)

	_, err = phpApiFromExtensionDir("/usr/lib/php/extensions/debug")
	assert.Error(t, err)
}

func TestCreatesMaintainerScripts(t *testing.T) {
	assert.Contains(t, postinstScript("foo", "8.3"), "phpenmod -v '8.3' 'foo' || true")
	assert.Contains(t, prermScript("foo", "8.3"), "phpdismod -v '8.3' 'foo' || true")
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
	require.NoError(t, err)
	defer os.Remove(tmpSo.Name())
	defer tmpSo.Close()

	_, err = tmpSo.Write([]byte("fake-so"))
	require.NoError(t, err)

	tmpDeb, err := os.CreateTemp("", "output-deb-*")
	require.NoError(t, err)
	defer os.Remove(tmpDeb.Name())
	tmpDeb.Close()

	err = CreateDeb(tmpSo.Name(), tmpDeb.Name(), d)
	assert.NoError(t, err)

	pkg, err := os.ReadFile(tmpDeb.Name())
	require.NoError(t, err)

	members, err := parseAr(pkg)
	require.NoError(t, err)

	assert.Equal(t, "2.0\n", string(members["debian-binary"]))

	controlFiles, err := tarFiles(members["control.tar.gz"])
	require.NoError(t, err)

	assert.Contains(t, string(controlFiles["control"]), "Package: php8.3-foo")
	assert.Contains(t, controlFiles, "postinst")
	assert.Contains(t, controlFiles, "prerm")

	dataFiles, err := tarFiles(members["data.tar.gz"])
	require.NoError(t, err)

	assert.Equal(t, "fake-so", string(dataFiles["usr/lib/php/20230831/foo.so"]))
	assert.Equal(t, "extension=foo.so\n", string(dataFiles["etc/php/8.3/mods-available/foo.ini"]))
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
		name := path.Clean(header.Name)
		name = strings.TrimPrefix(name, ".")
		name = strings.TrimPrefix(name, "/")
		files[name] = content.Bytes()
	}
	return files, nil
}
