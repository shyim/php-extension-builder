package cmd

import (
	"fmt"

	"github.com/shyim/php-extension-builder/internal/builder"
	"github.com/spf13/cobra"
)

var buildArgs builder.BuildArgs

var buildCmd = &cobra.Command{
	Use:          "build",
	Short:        "Build and package a PHP extension",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		outputPaths, err := builder.Build(buildArgs)
		if err != nil {
			return err
		}

		for _, path := range outputPaths {
			fmt.Println(path)
		}
		return nil
	},
}

func init() {
	buildCmd.Flags().StringVar(&buildArgs.PackageVersion, "package-version", "", "Package version (required)")
	buildCmd.MarkFlagRequired("package-version")

	buildCmd.Flags().StringSliceVar(&buildArgs.Artifact, "artifact", nil, "Artifact formats to generate (zip, deb)")
	buildCmd.Flags().StringVar(&buildArgs.PhpVersion, "php-version", "", "PHP version target")
	buildCmd.Flags().StringVar(&buildArgs.TargetOs, "target-os", "linux", "Target operating system (linux, darwin)")
	buildCmd.Flags().StringVar(&buildArgs.Libc, "libc", "", "Libc flavor (glibc, musl, bsdlibc)")
	buildCmd.Flags().BoolVar(&buildArgs.Zts, "zts", false, "Enable Zend Thread Safety")
	buildCmd.Flags().StringVar(&buildArgs.BuildPath, "build-path", ".", "Path to extension directory")
	buildCmd.Flags().StringSliceVar(&buildArgs.ConfigureFlag, "configure-flag", nil, "Configure flags")
	buildCmd.Flags().StringSliceVar(&buildArgs.BeforePhpizeCommand, "before-phpize-command", nil, "Commands to run before phpize")
	buildCmd.Flags().StringSliceVar(&buildArgs.AptPackage, "apt-package", nil, "Apt packages to install")
	buildCmd.Flags().StringSliceVar(&buildArgs.ApkPackage, "apk-package", nil, "Apk packages to install")
	buildCmd.Flags().StringVar(&buildArgs.OutDir, "out-dir", ".", "Output directory")
	buildCmd.Flags().StringVar(&buildArgs.Image, "image", "", "Docker image override")
	buildCmd.Flags().StringVar(&buildArgs.PhpConfig, "php-config", "", "Path to php-config")
	buildCmd.Flags().StringVar(&buildArgs.BuildKind, "build-kind", "", "Build kind (c, rust)")
	buildCmd.Flags().StringSliceVar(&buildArgs.CargoFeature, "cargo-feature", nil, "Cargo features to enable")

	rootCmd.AddCommand(buildCmd)
}
