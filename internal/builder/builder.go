package builder

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shyim/php-extension-builder/internal/backend"
	"github.com/shyim/php-extension-builder/internal/composer"
	"github.com/shyim/php-extension-builder/internal/config"
	"github.com/shyim/php-extension-builder/internal/packager"
	"github.com/shyim/php-extension-builder/internal/rustext"
)

type BuildArgs struct {
	PackageVersion      string
	Artifact            []string
	PhpVersion          string
	TargetOs            string
	Libc                string
	Zts                 bool
	BuildPath           string
	ConfigureFlag       []string
	BeforePhpizeCommand []string
	AptPackage          []string
	ApkPackage          []string
	OutDir              string
	Image               string
	PhpConfig           string
	BuildKind           string
	CargoFeature        []string
}

func Build(args BuildArgs) ([]string, error) {
	cfg, err := ResolveConfig(args)
	if err != nil {
		return nil, err
	}

	composerJsonPath := filepath.Join(cfg.BuildPath, "composer.json")
	extensionName, err := composer.ExtensionNameFromFile(composerJsonPath)
	if err != nil {
		return nil, err
	}

	var metadata *backend.BuildMetadata
	switch cfg.TargetOs {
	case config.OsLinux:
		metadata, err = backend.DockerLinux{}.Build(cfg)
	case config.OsDarwin:
		metadata, err = backend.NativeDarwin{}.Build(cfg)
	default:
		return nil, fmt.Errorf("unsupported target OS: %s", cfg.TargetOs)
	}
	if err != nil {
		return nil, err
	}

	soPath, err := resolveSoPath(cfg, extensionName)
	if err != nil {
		return nil, err
	}

	var outputPaths []string
	for _, artifact := range cfg.Artifacts {
		switch artifact {
		case config.ArtifactZip:
			pkgDetails := packager.PackageDetails{
				ExtensionName:  extensionName,
				PackageVersion: cfg.PackageVersion,
				PhpMajorMinor:  metadata.PhpMajorMinor,
				Arch:           metadata.Arch,
				Os:             string(cfg.TargetOs),
				Libc:           string(cfg.Libc),
				DebugSuffix:    metadata.DebugSuffix,
				ZtsSuffix:      metadata.ZtsSuffix,
			}
			outPath := filepath.Join(cfg.OutDir, pkgDetails.Filename())
			err := packager.CreateZip(soPath, outPath, fmt.Sprintf("%s.so", extensionName))
			if err != nil {
				return nil, fmt.Errorf("failed to package %s: %w", soPath, err)
			}
			outputPaths = append(outputPaths, outPath)

		case config.ArtifactDeb:
			pkgDetails := packager.DebPackageDetails{
				ExtensionName:  extensionName,
				PackageVersion: cfg.PackageVersion,
				PhpMajorMinor:  metadata.PhpMajorMinor,
				Arch:           metadata.Arch,
				ExtensionDir:   metadata.ExtensionDir,
			}
			fn, err := pkgDetails.Filename()
			if err != nil {
				return nil, err
			}
			outPath := filepath.Join(cfg.OutDir, fn)
			err = packager.CreateDeb(soPath, outPath, pkgDetails)
			if err != nil {
				return nil, fmt.Errorf("failed to package %s: %w", soPath, err)
			}
			outputPaths = append(outputPaths, outPath)
		}
	}

	return outputPaths, nil
}

func ResolveConfig(args BuildArgs) (*config.BuildConfig, error) {
	artifacts := selectedArtifacts(args.Artifact)

	targetOs := config.TargetOs(args.TargetOs)
	if targetOs != config.OsLinux && targetOs != config.OsDarwin {
		return nil, fmt.Errorf("invalid target OS: %s", args.TargetOs)
	}

	libc := config.Libc(args.Libc)
	if libc == "" {
		if targetOs == config.OsLinux {
			libc = config.LibcGlibc
		} else {
			libc = config.LibcBsdlibc
		}
	}

	// Platform checks
	if targetOs == config.OsLinux && libc == config.LibcBsdlibc {
		return nil, fmt.Errorf("linux builds support only glibc or musl libc targets")
	}
	if targetOs == config.OsDarwin && (libc == config.LibcGlibc || libc == config.LibcMusl) {
		return nil, fmt.Errorf("darwin builds support only bsdlibc")
	}

	if targetOs == config.OsLinux && args.PhpVersion == "" {
		return nil, fmt.Errorf("--php-version is required for linux Docker builds")
	}

	if targetOs == config.OsDarwin && args.Image != "" {
		return nil, fmt.Errorf("--image is only supported for linux Docker builds")
	}

	if targetOs == config.OsDarwin && (len(args.AptPackage) > 0 || len(args.ApkPackage) > 0) {
		return nil, fmt.Errorf("--apt-package and --apk-package are only supported for linux Docker builds")
	}

	// Determine BuildKind
	var extensionKind config.BuildKind
	if args.BuildKind != "" {
		extensionKind = config.BuildKind(args.BuildKind)
	} else {
		isRust, err := rustext.IsRustExtension(args.BuildPath)
		if err != nil {
			return nil, err
		}
		if isRust {
			extensionKind = config.BuildKindRust
		} else {
			extensionKind = config.BuildKindC
		}
	}

	if extensionKind == config.BuildKindRust {
		if len(args.ConfigureFlag) > 0 {
			return nil, fmt.Errorf("--configure-flag is not supported for Rust builds; use --cargo-feature instead")
		}
	} else {
		if len(args.CargoFeature) > 0 {
			return nil, fmt.Errorf("--cargo-feature is only supported for Rust builds")
		}
	}

	// Validate deb artifact requirements
	hasDeb := false
	for _, art := range artifacts {
		if art == config.ArtifactDeb {
			hasDeb = true
			break
		}
	}

	if hasDeb {
		if targetOs != config.OsLinux {
			return nil, fmt.Errorf("--artifact deb is only supported for linux builds")
		}
		if libc != config.LibcGlibc {
			return nil, fmt.Errorf("--artifact deb is only supported for glibc linux builds")
		}
		if args.Zts {
			return nil, fmt.Errorf("--artifact deb is only supported for non-ZTS linux builds")
		}
	}

	return &config.BuildConfig{
		PackageVersion:       args.PackageVersion,
		Artifacts:            artifacts,
		PhpVersion:           args.PhpVersion,
		TargetOs:             targetOs,
		Libc:                 libc,
		Zts:                  args.Zts,
		BuildPath:            args.BuildPath,
		ConfigureFlags:       args.ConfigureFlag,
		BeforePhpizeCommands: args.BeforePhpizeCommand,
		AptPackages:          args.AptPackage,
		ApkPackages:          args.ApkPackage,
		OutDir:               args.OutDir,
		Image:                args.Image,
		PhpConfig:            args.PhpConfig,
		ExtensionKind:        extensionKind,
		CargoFeatures:        args.CargoFeature,
	}, nil
}

func selectedArtifacts(artifacts []string) []config.ArtifactKind {
	if len(artifacts) == 0 {
		return []config.ArtifactKind{config.ArtifactZip}
	}

	var selected []config.ArtifactKind
	seen := make(map[config.ArtifactKind]bool)
	for _, art := range artifacts {
		kind := config.ArtifactKind(art)
		if (kind == config.ArtifactZip || kind == config.ArtifactDeb) && !seen[kind] {
			seen[kind] = true
			selected = append(selected, kind)
		}
	}
	return selected
}

func resolveSoPath(cfg *config.BuildConfig, extensionName string) (string, error) {
	modules := filepath.Join(cfg.BuildPath, "modules", fmt.Sprintf("%s.so", extensionName))

	if cfg.ExtensionKind == config.BuildKindC {
		return modules, nil
	}

	candidates := []string{
		modules,
		filepath.Join(cfg.BuildPath, fmt.Sprintf("%s.so", extensionName)),
	}

	crateName := rustext.CrateName(cfg.BuildPath)
	if crateName != "" {
		libraries := []string{
			fmt.Sprintf("lib%s.so", crateName),
			fmt.Sprintf("lib%s.dylib", crateName),
		}

		for _, lib := range libraries {
			candidates = append(candidates, filepath.Join(cfg.BuildPath, "target", "release", lib))
			workspaceTarget := workspaceTargetDir(cfg.BuildPath)
			if workspaceTarget != "" {
				candidates = append(candidates, filepath.Join(workspaceTarget, "release", lib))
			}
		}
	}

	for _, cand := range candidates {
		info, err := os.Stat(cand)
		if err == nil && !info.IsDir() {
			return cand, nil
		}
	}

	return "", fmt.Errorf("could not locate the built .so for Rust extension '%s' under %s", extensionName, cfg.BuildPath)
}

func workspaceTargetDir(buildPath string) string {
	abs, err := filepath.Abs(buildPath)
	if err != nil {
		return ""
	}

	curr := filepath.Dir(abs)
	for {
		target := filepath.Join(curr, "target")
		info, err := os.Stat(target)
		if err == nil && info.IsDir() {
			return target
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}
	return ""
}
