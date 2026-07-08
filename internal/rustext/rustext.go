package rustext

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	extPhpRsDepRegex = regexp.MustCompile(`(?m)^\s*(ext-php-rs\s*(=|\.)|\[[^\]]*\bdependencies\.ext-php-rs\b)`)
	tableHeaderRegex = regexp.MustCompile(`^\s*\[([^\]]+)\]`)
	nameRegex        = regexp.MustCompile(`^\s*name\s*=\s*"([^"]+)"`)
)

func IsRustExtension(buildPath string) (bool, error) {
	cargo := filepath.Join(buildPath, "Cargo.toml")
	_, err := os.Stat(cargo)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	contents, err := os.ReadFile(cargo)
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", cargo, err)
	}

	return DeclaresExtPhpRs(string(contents)), nil
}

func CrateName(buildPath string) string {
	contents, err := os.ReadFile(filepath.Join(buildPath, "Cargo.toml"))
	if err != nil {
		return ""
	}

	name := CdylibName(string(contents))
	if name == "" {
		return ""
	}
	return strings.ReplaceAll(name, "-", "_")
}

func DeclaresExtPhpRs(contents string) bool {
	return extPhpRsDepRegex.MatchString(contents)
}

func CdylibName(contents string) string {
	var currentTable string
	var packageName string
	var libName string

	lines := strings.Split(contents, "\n")
	for _, line := range lines {
		if m := tableHeaderRegex.FindStringSubmatch(line); m != nil {
			currentTable = strings.TrimSpace(m[1])
			continue
		}

		if currentTable != "package" && currentTable != "lib" {
			continue
		}

		if m := nameRegex.FindStringSubmatch(line); m != nil {
			value := m[1]
			if currentTable == "lib" {
				libName = value
			} else {
				packageName = value
			}
		}
	}

	if libName != "" {
		return libName
	}
	return packageName
}
