package composer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRejectsMissingOrInvalidType(t *testing.T) {
	_, err := ExtensionNameFromJSON([]byte(`{"type":"library"}`))
	assert.ErrorContains(t, err, `composer.json type must be "php-ext" or "php-ext-zend", but "library" was found`)
}

func TestPrefersPhpExtExtensionNameAndStripsExtPrefix(t *testing.T) {
	name, err := ExtensionNameFromJSON([]byte(
		`{"type":"php-ext","php-ext":{"extension-name":"ext-test_ext"},"name":"vendor/ignored"}`,
	))
	assert.NoError(t, err)
	assert.Equal(t, "test_ext", name)
}

func TestFallsBackToPackageNameSuffix(t *testing.T) {
	name, err := ExtensionNameFromJSON([]byte(`{"type":"php-ext-zend","name":"vendor/foo"}`))
	assert.NoError(t, err)
	assert.Equal(t, "foo", name)
}

func TestRejectsMissingExtensionAndPackageNames(t *testing.T) {
	_, err := ExtensionNameFromJSON([]byte(`{"type":"php-ext"}`))
	assert.ErrorContains(t, err, `could not determine extension name: both ."php-ext"."extension-name" and .name are missing in composer.json`)
}

func TestRejectsInvalidExtensionName(t *testing.T) {
	_, err := ExtensionNameFromJSON([]byte(
		`{"type":"php-ext","php-ext":{"extension-name":"invalid-ext-name"}}`,
	))
	assert.ErrorContains(t, err, `invalid extension name: "invalid-ext-name" - must be alphanumeric/underscores only`)
}
