package composer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var extensionNameRegex = regexp.MustCompile(`^[A-Za-z][a-zA-Z0-9_]+$`)

type PhpExt struct {
	ExtensionName string `json:"extension-name"`
}

type ComposerJson struct {
	Type   string  `json:"type"`
	Name   string  `json:"name"`
	PhpExt *PhpExt `json:"php-ext"`
}

func ExtensionNameFromFile(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		// Matching Rust error: "{} not found. This does not appear to be a PIE package."
		return "", fmt.Errorf("%s not found; this does not appear to be a PIE package", filepath.Base(path))
	}
	return ExtensionNameFromJSON(contents)
}

func ExtensionNameFromJSON(contents []byte) (string, error) {
	var composer ComposerJson
	if err := json.Unmarshal(contents, &composer); err != nil {
		return "", fmt.Errorf("failed to parse composer.json")
	}

	packageType := composer.Type
	if packageType == "" {
		packageType = "null"
	}
	if packageType != "php-ext" && packageType != "php-ext-zend" {
		return "", fmt.Errorf("composer.json type must be \"php-ext\" or \"php-ext-zend\", but \"%s\" was found", packageType)
	}

	var extensionName string
	if composer.PhpExt != nil {
		extensionName = composer.PhpExt.ExtensionName
	}

	if extensionName == "" {
		packageName := composer.Name
		if packageName == "" {
			return "", fmt.Errorf("could not determine extension name: both .\"php-ext\".\"extension-name\" and .name are missing in composer.json")
		}

		parts := strings.Split(packageName, "/")
		extensionName = parts[len(parts)-1]
	}

	extensionName = strings.TrimPrefix(extensionName, "ext-")

	if !extensionNameRegex.MatchString(extensionName) {
		return "", fmt.Errorf("invalid extension name: \"%s\" - must be alphanumeric/underscores only", extensionName)
	}

	return extensionName, nil
}
