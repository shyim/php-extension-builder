package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
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
	assert.NoError(t, err)
	assert.Equal(t, []string{"--enable-example-pie-extension", "--with-hello-name=FROM_CLI"}, buildArgs.ConfigureFlag)
}

func TestAcceptsCustomAptAndApkPackages(t *testing.T) {
	err := runBuildCmd([]string{
		"build",
		"--package-version", "1.2.3",
		"--php-version", "8.3",
		"--apt-package", "libzstd-dev",
		"--apk-package", "zstd-dev",
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"libzstd-dev"}, buildArgs.AptPackage)
	assert.Equal(t, []string{"zstd-dev"}, buildArgs.ApkPackage)
}

func TestAcceptsRepeatedBeforePhpizeCommands(t *testing.T) {
	err := runBuildCmd([]string{
		"build",
		"--package-version", "1.2.3",
		"--php-version", "8.3",
		"--before-phpize-command", "composer install --no-dev",
		"--before-phpize-command", "./autogen.sh --force",
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"composer install --no-dev", "./autogen.sh --force"}, buildArgs.BeforePhpizeCommand)
}

func TestAcceptsRepeatedCargoFeatures(t *testing.T) {
	err := runBuildCmd([]string{
		"build",
		"--package-version", "1.2.3",
		"--php-version", "8.3",
		"--cargo-feature", "closure",
		"--cargo-feature", "anyhow",
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"closure", "anyhow"}, buildArgs.CargoFeature)
}

func TestAcceptsRepeatedArtifacts(t *testing.T) {
	err := runBuildCmd([]string{
		"build",
		"--package-version", "1.2.3",
		"--php-version", "8.3",
		"--artifact", "zip",
		"--artifact", "deb",
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"zip", "deb"}, buildArgs.Artifact)
}
