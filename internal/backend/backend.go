package backend

import (
	"fmt"
	"strings"

	"github.com/shyim/php-extension-builder/internal/config"
	"github.com/shyim/php-extension-builder/internal/packager"
)

type BuildMetadata struct {
	PhpMajorMinor string
	Arch          string
	ExtensionDir  string
	DebugSuffix   string
	ZtsSuffix     string
}

type BuildBackend interface {
	Build(cfg *config.BuildConfig) (*BuildMetadata, error)
}

func ValidateRequestedMetadata(cfg *config.BuildConfig, metadata *BuildMetadata) error {
	if cfg.PhpVersion != "" {
		requestedPhpMajorMinor := packager.PhpMajorMinor(cfg.PhpVersion)
		if requestedPhpMajorMinor != metadata.PhpMajorMinor {
			return fmt.Errorf("requested PHP %s, but selected PHP reports %s", requestedPhpMajorMinor, metadata.PhpMajorMinor)
		}
	}

	actualZts := metadata.ZtsSuffix == "-zts"
	if cfg.Zts && !actualZts {
		return fmt.Errorf("--zts was requested, but selected PHP is non-ZTS")
	}
	if !cfg.Zts && actualZts {
		return fmt.Errorf("non-ZTS build was requested, but selected PHP is ZTS; pass --zts")
	}

	return nil
}

func normalizeArch(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "x86_64", "amd64":
		return "x86_64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	case "i386", "i686", "x86":
		return "x86", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", value)
	}
}
