package cmd

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func runBuildCmd(args []string) error {
	// Reset buildArgs
	buildArgs.PackageVersion = ""
	buildArgs.Artifact = nil
	buildArgs.PhpVersion = ""
	buildArgs.TargetOs = "linux" // default
	buildArgs.Libc = ""
	buildArgs.Zts = false
	buildArgs.BuildPath = "." // default
	buildArgs.ConfigureFlag = nil
	buildArgs.BeforePhpizeCommand = nil
	buildArgs.AptPackage = nil
	buildArgs.ApkPackage = nil
	buildArgs.OutDir = "." // default
	buildArgs.Image = ""
	buildArgs.PhpConfig = ""
	buildArgs.BuildKind = ""
	buildArgs.CargoFeature = nil

	oldRunE := buildCmd.RunE
	defer func() { buildCmd.RunE = oldRunE }()
	buildCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return nil
	}

	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

func TestAcceptsRepeatedHyphenatedConfigureFlags(t *testing.T) {
	err := runBuildCmd([]string{
		"build",
		"--package-version", "1.2.3",
		"--php-version", "8.3",
		"--configure-flag", "--enable-example-pie-extension",
		"--configure-flag", "--with-hello-name=FROM_CLI",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"--enable-example-pie-extension", "--with-hello-name=FROM_CLI"}
	if !reflect.DeepEqual(buildArgs.ConfigureFlag, expected) {
		t.Errorf("expected %v, got %v", expected, buildArgs.ConfigureFlag)
	}
}

func TestAcceptsCustomAptAndApkPackages(t *testing.T) {
	err := runBuildCmd([]string{
		"build",
		"--package-version", "1.2.3",
		"--php-version", "8.3",
		"--apt-package", "libzstd-dev",
		"--apk-package", "zstd-dev",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedApt := []string{"libzstd-dev"}
	expectedApk := []string{"zstd-dev"}

	if !reflect.DeepEqual(buildArgs.AptPackage, expectedApt) {
		t.Errorf("expected apt package %v, got %v", expectedApt, buildArgs.AptPackage)
	}
	if !reflect.DeepEqual(buildArgs.ApkPackage, expectedApk) {
		t.Errorf("expected apk package %v, got %v", expectedApk, buildArgs.ApkPackage)
	}
}

func TestAcceptsRepeatedBeforePhpizeCommands(t *testing.T) {
	err := runBuildCmd([]string{
		"build",
		"--package-version", "1.2.3",
		"--php-version", "8.3",
		"--before-phpize-command", "composer install --no-dev",
		"--before-phpize-command", "./autogen.sh --force",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"composer install --no-dev", "./autogen.sh --force"}
	if !reflect.DeepEqual(buildArgs.BeforePhpizeCommand, expected) {
		t.Errorf("expected %v, got %v", expected, buildArgs.BeforePhpizeCommand)
	}
}

func TestAcceptsRepeatedCargoFeatures(t *testing.T) {
	err := runBuildCmd([]string{
		"build",
		"--package-version", "1.2.3",
		"--php-version", "8.3",
		"--cargo-feature", "closure",
		"--cargo-feature", "anyhow",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"closure", "anyhow"}
	if !reflect.DeepEqual(buildArgs.CargoFeature, expected) {
		t.Errorf("expected %v, got %v", expected, buildArgs.CargoFeature)
	}
}

func TestAcceptsRepeatedArtifacts(t *testing.T) {
	err := runBuildCmd([]string{
		"build",
		"--package-version", "1.2.3",
		"--php-version", "8.3",
		"--artifact", "zip",
		"--artifact", "deb",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"zip", "deb"}
	if !reflect.DeepEqual(buildArgs.Artifact, expected) {
		t.Errorf("expected %v, got %v", expected, buildArgs.Artifact)
	}
}
