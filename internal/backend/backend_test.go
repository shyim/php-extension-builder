package backend

import (
	"runtime"
	"testing"

	"github.com/shyim/php-extension-builder/internal/config"
	"github.com/stretchr/testify/assert"
)

func linuxConfig(libc config.Libc, zts bool) *config.BuildConfig {
	return &config.BuildConfig{
		PackageVersion:       "1.2.3",
		Artifacts:            []config.ArtifactKind{config.ArtifactZip},
		PhpVersion:           "8.3",
		TargetOs:             config.OsLinux,
		Libc:                 libc,
		Zts:                  zts,
		BuildPath:            ".",
		ConfigureFlags:       nil,
		BeforePhpizeCommands: nil,
		AptPackages:          nil,
		ApkPackages:          nil,
		OutDir:               ".",
		Image:                "",
		PhpConfig:            "",
		ExtensionKind:        config.BuildKindC,
		CargoFeatures:        nil,
	}
}

func TestSelectsOfficialPhpImages(t *testing.T) {
	img, err := dockerImage(linuxConfig(config.LibcGlibc, false))
	assert.NoError(t, err)
	assert.Equal(t, "php:8.3-cli", img)

	img, err = dockerImage(linuxConfig(config.LibcGlibc, true))
	assert.NoError(t, err)
	assert.Equal(t, "php:8.3-zts", img)

	img, err = dockerImage(linuxConfig(config.LibcMusl, false))
	assert.NoError(t, err)
	assert.Equal(t, "php:8.3-cli-alpine", img)

	img, err = dockerImage(linuxConfig(config.LibcMusl, true))
	assert.NoError(t, err)
	assert.Equal(t, "php:8.3-zts-alpine", img)
}

func TestSelectsRustGhcrImages(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.ExtensionKind = config.BuildKindRust
	img, err := dockerImage(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "ghcr.io/shyim/php-extension-builder-rust:8.3-cli", img)

	cfg2 := linuxConfig(config.LibcMusl, true)
	cfg2.ExtensionKind = config.BuildKindRust
	img, err = dockerImage(cfg2)
	assert.NoError(t, err)
	assert.Equal(t, "ghcr.io/shyim/php-extension-builder-rust:8.3-zts-alpine", img)
}

func TestRustImageRespectsExplicitOverride(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.ExtensionKind = config.BuildKindRust
	cfg.Image = "ghcr.io/acme/custom:8.3"
	img, err := dockerImage(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "ghcr.io/acme/custom:8.3", img)
}

func TestRustScriptBranchesOnPieAndRunsCargo(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.ExtensionKind = config.BuildKindRust
	cfg.CargoFeatures = []string{"closure", "anyhow"}
	script := dockerScript(cfg)

	assert.Contains(t, script, "if [ -f config.m4 ] || [ -f pie/config.m4 ]; then")
	assert.Contains(t, script, "cargo build --release --features 'closure,anyhow'")
	assert.Contains(t, script, "clang")
}

func TestRustScriptWithoutFeaturesRunsPlainCargo(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.ExtensionKind = config.BuildKindRust
	script := dockerScript(cfg)

	assert.Contains(t, script, "\n  cargo build --release\n")
}

func TestDockerWorkdirIsUnderWorkspace(t *testing.T) {
	wd, err := dockerWorkdir(".")
	assert.NoError(t, err)
	assert.Equal(t, "/workspace", wd)

	wd, err = dockerWorkdir("src/php/ext/grpc")
	assert.NoError(t, err)
	assert.Equal(t, "/workspace/src/php/ext/grpc", wd)
}

func TestDockerWorkdirRejectsAbsolutePaths(t *testing.T) {
	absPath := "/tmp/ext"
	if runtime.GOOS == "windows" {
		absPath = "C:\\tmp\\ext"
	}
	_, err := dockerWorkdir(absPath)
	assert.Error(t, err)
}

func TestDockerScriptQuotesConfigureFlags(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.ConfigureFlags = []string{"--enable-test", "--with-name=O'Hara"}
	script := dockerScript(cfg)

	assert.Contains(t, script, "./configure '--enable-test' '--with-name=O'\\''Hara'")
}

func TestDockerScriptAddsCustomDistroPackages(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.AptPackages = []string{"libzstd-dev", "libfoo=1.2"}
	cfg.ApkPackages = []string{"zstd-dev", "foo-dev"}
	script := dockerScript(cfg)

	assert.Contains(t, script, "apk add --no-cache ${PHPIZE_DEPS:-autoconf dpkg-dev dpkg file g++ gcc libc-dev make pkgconf re2c} 'zstd-dev' 'foo-dev'")
	assert.Contains(t, script, "apt-get install -y --no-install-recommends ${PHPIZE_DEPS:-autoconf dpkg-dev file g++ gcc libc-dev make pkg-config re2c} 'libzstd-dev' 'libfoo=1.2'")
}

func TestDockerScriptRunsBeforePhpizeCommands(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.BeforePhpizeCommands = []string{"composer install --no-dev", "./autogen.sh --force"}
	script := dockerScript(cfg)

	assert.Contains(t, script, "echo '==> Running before phpize command: composer install --no-dev' >&2\ncomposer install --no-dev")
	assert.Contains(t, script, "echo '==> Running before phpize command: ./autogen.sh --force' >&2\n./autogen.sh --force")
	assert.Contains(t, script, "./autogen.sh --force\necho \"==> Running phpize\" >&2\nphpize")
}

func TestNormalizesArchitectureNames(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"x86_64", "x86_64"},
		{"amd64", "x86_64"},
		{"aarch64", "arm64"},
		{"arm64", "arm64"},
		{"i686", "x86"},
	}

	for _, tc := range tests {
		res, err := normalizeArch(tc.input)
		assert.NoError(t, err)
		assert.Equal(t, tc.expected, res)
	}
}

func TestValidatesRequestedPhpVersion(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	metadata := &BuildMetadata{
		PhpMajorMinor: "8.2",
		Arch:          "arm64",
		ExtensionDir:  "/usr/local/lib/php/extensions/no-debug-non-zts-20230831",
	}

	err := ValidateRequestedMetadata(cfg, metadata)
	assert.ErrorContains(t, err, "requested PHP 8.3, but selected PHP reports 8.2")
}

func TestValidatesRequestedZtsMode(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, true)
	metadata := &BuildMetadata{
		PhpMajorMinor: "8.3",
		Arch:          "arm64",
		ExtensionDir:  "/usr/local/lib/php/extensions/no-debug-non-zts-20230831",
	}

	err := ValidateRequestedMetadata(cfg, metadata)
	assert.ErrorContains(t, err, "--zts was requested, but selected PHP is non-ZTS")
}

func TestValidatesImplicitNtsMode(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	metadata := &BuildMetadata{
		PhpMajorMinor: "8.3",
		Arch:          "arm64",
		ExtensionDir:  "/usr/local/lib/php/extensions/no-debug-zts-20230831",
		ZtsSuffix:     "-zts",
	}

	err := ValidateRequestedMetadata(cfg, metadata)
	assert.ErrorContains(t, err, "non-ZTS build was requested, but selected PHP is ZTS; pass --zts")
}
