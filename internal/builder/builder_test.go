package builder

import (
	"testing"

	"github.com/shyim/php-extension-builder/internal/config"
	"github.com/stretchr/testify/assert"
)

func defaultArgs(targetOs string) BuildArgs {
	return BuildArgs{
		PackageVersion:      "1.2.3",
		Artifact:            nil,
		PhpVersion:          "8.3",
		TargetOs:            targetOs,
		Libc:                "",
		Zts:                 false,
		BuildPath:           ".",
		ConfigureFlag:       nil,
		BeforePhpizeCommand: nil,
		AptPackage:          nil,
		ApkPackage:          nil,
		OutDir:              ".",
		Image:               "",
		PhpConfig:           "",
		BuildKind:           "",
		CargoFeature:        nil,
	}
}

func TestDefaultsLinuxToGlibc(t *testing.T) {
	cfg, err := ResolveConfig(defaultArgs("linux"))
	assert.NoError(t, err)
	assert.Equal(t, config.LibcGlibc, cfg.Libc)
	assert.Equal(t, []config.ArtifactKind{config.ArtifactZip}, cfg.Artifacts)
}

func TestDefaultsDarwinToBsdlibc(t *testing.T) {
	args := defaultArgs("darwin")
	args.PhpVersion = ""
	cfg, err := ResolveConfig(args)
	assert.NoError(t, err)
	assert.Equal(t, config.LibcBsdlibc, cfg.Libc)
}

func TestDefaultsArtifactsToZip(t *testing.T) {
	artifacts := selectedArtifacts(nil)
	assert.Equal(t, []config.ArtifactKind{config.ArtifactZip}, artifacts)
}

func TestPreservesArtifactOrderAndRemovesDuplicates(t *testing.T) {
	artifacts := selectedArtifacts([]string{"deb", "zip", "deb"})
	assert.Equal(t, []config.ArtifactKind{config.ArtifactDeb, config.ArtifactZip}, artifacts)
}

func TestRequiresPhpVersionForLinux(t *testing.T) {
	args := defaultArgs("linux")
	args.PhpVersion = ""

	_, err := ResolveConfig(args)
	assert.ErrorContains(t, err, "--php-version is required for linux Docker builds")
}

func TestRejectsDarwinDockerImageOverride(t *testing.T) {
	args := defaultArgs("darwin")
	args.Image = "php:8.3-cli"

	_, err := ResolveConfig(args)
	assert.ErrorContains(t, err, "--image is only supported for linux Docker builds")
}

func TestRejectsDarwinContainerPackages(t *testing.T) {
	args := defaultArgs("darwin")
	args.AptPackage = []string{"libzstd-dev"}

	_, err := ResolveConfig(args)
	assert.ErrorContains(t, err, "--apt-package and --apk-package are only supported for linux Docker builds")
}

func TestRejectsDebForDarwin(t *testing.T) {
	args := defaultArgs("darwin")
	args.Artifact = []string{"deb"}

	_, err := ResolveConfig(args)
	assert.ErrorContains(t, err, "--artifact deb is only supported for linux builds")
}

func TestRejectsDebForMusl(t *testing.T) {
	args := defaultArgs("linux")
	args.Artifact = []string{"deb"}
	args.Libc = "musl"

	_, err := ResolveConfig(args)
	assert.ErrorContains(t, err, "--artifact deb is only supported for glibc linux builds")
}

func TestRejectsDebForZts(t *testing.T) {
	args := defaultArgs("linux")
	args.Artifact = []string{"deb"}
	args.Zts = true

	_, err := ResolveConfig(args)
	assert.ErrorContains(t, err, "--artifact deb is only supported for non-ZTS linux builds")
}

func TestDefaultsToCExtensionKind(t *testing.T) {
	cfg, err := ResolveConfig(defaultArgs("linux"))
	assert.NoError(t, err)
	assert.Equal(t, config.BuildKindC, cfg.ExtensionKind)
}

func TestHonorsExplicitRustBuildKind(t *testing.T) {
	args := defaultArgs("linux")
	args.BuildKind = "rust"
	args.CargoFeature = []string{"closure"}

	cfg, err := ResolveConfig(args)
	assert.NoError(t, err)
	assert.Equal(t, config.BuildKindRust, cfg.ExtensionKind)
	assert.Equal(t, []string{"closure"}, cfg.CargoFeatures)
}

func TestAllowsRustOnDarwin(t *testing.T) {
	args := defaultArgs("darwin")
	args.PhpVersion = ""
	args.BuildKind = "rust"

	cfg, err := ResolveConfig(args)
	assert.NoError(t, err)
	assert.Equal(t, config.BuildKindRust, cfg.ExtensionKind)
	assert.Equal(t, config.OsDarwin, cfg.TargetOs)
}

func TestRejectsConfigureFlagForRust(t *testing.T) {
	args := defaultArgs("linux")
	args.BuildKind = "rust"
	args.ConfigureFlag = []string{"--enable-foo"}

	_, err := ResolveConfig(args)
	assert.ErrorContains(t, err, "--configure-flag is not supported for Rust builds; use --cargo-feature instead")
}

func TestRejectsCargoFeatureForC(t *testing.T) {
	args := defaultArgs("linux")
	args.CargoFeature = []string{"closure"}

	_, err := ResolveConfig(args)
	assert.ErrorContains(t, err, "--cargo-feature is only supported for Rust builds")
}
