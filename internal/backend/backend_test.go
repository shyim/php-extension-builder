package backend

import (
	"runtime"
	"strings"
	"testing"

	"github.com/shyim/php-extension-builder/internal/config"
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
	if err != nil || img != "php:8.3-cli" {
		t.Errorf("expected php:8.3-cli, got %q (err: %v)", img, err)
	}

	img, err = dockerImage(linuxConfig(config.LibcGlibc, true))
	if err != nil || img != "php:8.3-zts" {
		t.Errorf("expected php:8.3-zts, got %q (err: %v)", img, err)
	}

	img, err = dockerImage(linuxConfig(config.LibcMusl, false))
	if err != nil || img != "php:8.3-cli-alpine" {
		t.Errorf("expected php:8.3-cli-alpine, got %q (err: %v)", img, err)
	}

	img, err = dockerImage(linuxConfig(config.LibcMusl, true))
	if err != nil || img != "php:8.3-zts-alpine" {
		t.Errorf("expected php:8.3-zts-alpine, got %q (err: %v)", img, err)
	}
}

func TestSelectsRustGhcrImages(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.ExtensionKind = config.BuildKindRust
	img, err := dockerImage(cfg)
	if err != nil || img != "ghcr.io/shyim/php-extension-builder-rust:8.3-cli" {
		t.Errorf("expected Rust glibc image, got %q (err: %v)", img, err)
	}

	cfg2 := linuxConfig(config.LibcMusl, true)
	cfg2.ExtensionKind = config.BuildKindRust
	img, err = dockerImage(cfg2)
	if err != nil || img != "ghcr.io/shyim/php-extension-builder-rust:8.3-zts-alpine" {
		t.Errorf("expected Rust musl ZTS image, got %q (err: %v)", img, err)
	}
}

func TestRustImageRespectsExplicitOverride(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.ExtensionKind = config.BuildKindRust
	cfg.Image = "ghcr.io/acme/custom:8.3"
	img, err := dockerImage(cfg)
	if err != nil || img != "ghcr.io/acme/custom:8.3" {
		t.Errorf("expected override, got %q (err: %v)", img, err)
	}
}

func TestRustScriptBranchesOnPieAndRunsCargo(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.ExtensionKind = config.BuildKindRust
	cfg.CargoFeatures = []string{"closure", "anyhow"}
	script := dockerScript(cfg)

	if !strings.Contains(script, "if [ -f config.m4 ] || [ -f pie/config.m4 ]; then") {
		t.Error("missing config.m4 branch")
	}
	if !strings.Contains(script, "cargo build --release --features 'closure,anyhow'") {
		t.Error("missing features build command")
	}
	if !strings.Contains(script, "clang") {
		t.Error("missing clang dependency check or package install")
	}
}

func TestRustScriptWithoutFeaturesRunsPlainCargo(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.ExtensionKind = config.BuildKindRust
	script := dockerScript(cfg)

	if !strings.Contains(script, "\n  cargo build --release\n") {
		t.Error("missing plain cargo build command")
	}
}

func TestDockerWorkdirIsUnderWorkspace(t *testing.T) {
	wd, err := dockerWorkdir(".")
	if err != nil || wd != "/workspace" {
		t.Errorf("expected /workspace, got %q (err: %v)", wd, err)
	}

	wd, err = dockerWorkdir("src/php/ext/grpc")
	if err != nil || wd != "/workspace/src/php/ext/grpc" {
		t.Errorf("expected /workspace/src/php/ext/grpc, got %q (err: %v)", wd, err)
	}
}

func TestDockerWorkdirRejectsAbsolutePaths(t *testing.T) {
	absPath := "/tmp/ext"
	if runtime.GOOS == "windows" {
		absPath = "C:\\tmp\\ext"
	}
	_, err := dockerWorkdir(absPath)
	if err == nil {
		t.Error("expected error for absolute path, got nil")
	}
}

func TestDockerScriptQuotesConfigureFlags(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.ConfigureFlags = []string{"--enable-test", "--with-name=O'Hara"}
	script := dockerScript(cfg)

	if !strings.Contains(script, "./configure '--enable-test' '--with-name=O'\\''Hara'") {
		t.Errorf("missing quoted configure flags, got: %s", script)
	}
}

func TestDockerScriptAddsCustomDistroPackages(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.AptPackages = []string{"libzstd-dev", "libfoo=1.2"}
	cfg.ApkPackages = []string{"zstd-dev", "foo-dev"}
	script := dockerScript(cfg)

	if !strings.Contains(script, "apk add --no-cache ${PHPIZE_DEPS:-autoconf dpkg-dev dpkg file g++ gcc libc-dev make pkgconf re2c} 'zstd-dev' 'foo-dev'") {
		t.Error("missing alpine custom packages")
	}
	if !strings.Contains(script, "apt-get install -y --no-install-recommends ${PHPIZE_DEPS:-autoconf dpkg-dev file g++ gcc libc-dev make pkg-config re2c} 'libzstd-dev' 'libfoo=1.2'") {
		t.Error("missing debian custom packages")
	}
}

func TestDockerScriptRunsBeforePhpizeCommands(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, false)
	cfg.BeforePhpizeCommands = []string{"composer install --no-dev", "./autogen.sh --force"}
	script := dockerScript(cfg)

	if !strings.Contains(script, "echo '==> Running before phpize command: composer install --no-dev' >&2\ncomposer install --no-dev") {
		t.Error("missing first before command")
	}
	if !strings.Contains(script, "echo '==> Running before phpize command: ./autogen.sh --force' >&2\n./autogen.sh --force") {
		t.Error("missing second before command")
	}
	if !strings.Contains(script, "./autogen.sh --force\necho \"==> Running phpize\" >&2\nphpize") {
		t.Error("wrong execution order")
	}
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
		if err != nil || res != tc.expected {
			t.Errorf("normalizeArch(%q) = %q, %v; expected %q, nil", tc.input, res, err, tc.expected)
		}
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
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "requested PHP 8.3, but selected PHP reports 8.2"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestValidatesRequestedZtsMode(t *testing.T) {
	cfg := linuxConfig(config.LibcGlibc, true)
	metadata := &BuildMetadata{
		PhpMajorMinor: "8.3",
		Arch:          "arm64",
		ExtensionDir:  "/usr/local/lib/php/extensions/no-debug-non-zts-20230831",
	}

	err := ValidateRequestedMetadata(cfg, metadata)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "--zts was requested, but selected PHP is non-ZTS"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
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
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "non-ZTS build was requested, but selected PHP is ZTS; pass --zts"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}
