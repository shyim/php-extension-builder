package builder

import (
	"reflect"
	"testing"

	"github.com/shyim/php-extension-builder/internal/config"
)

func defaultArgs(targetOs string) BuildArgs {
	return BuildArgs{
		PackageVersion:       "1.2.3",
		Artifact:             nil,
		PhpVersion:           "8.3",
		TargetOs:             targetOs,
		Libc:                 "",
		Zts:                  false,
		BuildPath:            ".",
		ConfigureFlag:        nil,
		BeforePhpizeCommand: nil,
		AptPackage:           nil,
		ApkPackage:           nil,
		OutDir:               ".",
		Image:                "",
		PhpConfig:            "",
		BuildKind:            "",
		CargoFeature:         nil,
	}
}

func TestDefaultsLinuxToGlibc(t *testing.T) {
	cfg, err := ResolveConfig(defaultArgs("linux"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Libc != config.LibcGlibc {
		t.Errorf("expected libc glibc, got %s", cfg.Libc)
	}
	expectedArtifacts := []config.ArtifactKind{config.ArtifactZip}
	if !reflect.DeepEqual(cfg.Artifacts, expectedArtifacts) {
		t.Errorf("expected artifacts %v, got %v", expectedArtifacts, cfg.Artifacts)
	}
}

func TestDefaultsDarwinToBsdlibc(t *testing.T) {
	args := defaultArgs("darwin")
	args.PhpVersion = ""
	cfg, err := ResolveConfig(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Libc != config.LibcBsdlibc {
		t.Errorf("expected libc bsdlibc, got %s", cfg.Libc)
	}
}

func TestDefaultsArtifactsToZip(t *testing.T) {
	artifacts := selectedArtifacts(nil)
	expected := []config.ArtifactKind{config.ArtifactZip}
	if !reflect.DeepEqual(artifacts, expected) {
		t.Errorf("expected %v, got %v", expected, artifacts)
	}
}

func TestPreservesArtifactOrderAndRemovesDuplicates(t *testing.T) {
	artifacts := selectedArtifacts([]string{"deb", "zip", "deb"})
	expected := []config.ArtifactKind{config.ArtifactDeb, config.ArtifactZip}
	if !reflect.DeepEqual(artifacts, expected) {
		t.Errorf("expected %v, got %v", expected, artifacts)
	}
}

func TestRequiresPhpVersionForLinux(t *testing.T) {
	args := defaultArgs("linux")
	args.PhpVersion = ""

	_, err := ResolveConfig(args)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "--php-version is required for linux Docker builds"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestRejectsDarwinDockerImageOverride(t *testing.T) {
	args := defaultArgs("darwin")
	args.Image = "php:8.3-cli"

	_, err := ResolveConfig(args)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "--image is only supported for linux Docker builds"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestRejectsDarwinContainerPackages(t *testing.T) {
	args := defaultArgs("darwin")
	args.AptPackage = []string{"libzstd-dev"}

	_, err := ResolveConfig(args)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "--apt-package and --apk-package are only supported for linux Docker builds"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestRejectsDebForDarwin(t *testing.T) {
	args := defaultArgs("darwin")
	args.Artifact = []string{"deb"}

	_, err := ResolveConfig(args)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "--artifact deb is only supported for linux builds"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestRejectsDebForMusl(t *testing.T) {
	args := defaultArgs("linux")
	args.Artifact = []string{"deb"}
	args.Libc = "musl"

	_, err := ResolveConfig(args)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "--artifact deb is only supported for glibc linux builds"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestRejectsDebForZts(t *testing.T) {
	args := defaultArgs("linux")
	args.Artifact = []string{"deb"}
	args.Zts = true

	_, err := ResolveConfig(args)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "--artifact deb is only supported for non-ZTS linux builds"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestDefaultsToCExtensionKind(t *testing.T) {
	cfg, err := ResolveConfig(defaultArgs("linux"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ExtensionKind != config.BuildKindC {
		t.Errorf("expected build kind C, got %s", cfg.ExtensionKind)
	}
}

func TestHonorsExplicitRustBuildKind(t *testing.T) {
	args := defaultArgs("linux")
	args.BuildKind = "rust"
	args.CargoFeature = []string{"closure"}

	cfg, err := ResolveConfig(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ExtensionKind != config.BuildKindRust {
		t.Errorf("expected build kind Rust, got %s", cfg.ExtensionKind)
	}
	expectedFeatures := []string{"closure"}
	if !reflect.DeepEqual(cfg.CargoFeatures, expectedFeatures) {
		t.Errorf("expected cargo features %v, got %v", expectedFeatures, cfg.CargoFeatures)
	}
}

func TestAllowsRustOnDarwin(t *testing.T) {
	args := defaultArgs("darwin")
	args.PhpVersion = ""
	args.BuildKind = "rust"

	cfg, err := ResolveConfig(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ExtensionKind != config.BuildKindRust {
		t.Errorf("expected Rust extension kind, got %s", cfg.ExtensionKind)
	}
	if cfg.TargetOs != config.OsDarwin {
		t.Errorf("expected target OS Darwin, got %s", cfg.TargetOs)
	}
}

func TestRejectsConfigureFlagForRust(t *testing.T) {
	args := defaultArgs("linux")
	args.BuildKind = "rust"
	args.ConfigureFlag = []string{"--enable-foo"}

	_, err := ResolveConfig(args)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "--configure-flag is not supported for Rust builds; use --cargo-feature instead"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestRejectsCargoFeatureForC(t *testing.T) {
	args := defaultArgs("linux")
	args.CargoFeature = []string{"closure"}

	_, err := ResolveConfig(args)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := "--cargo-feature is only supported for Rust builds"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}
