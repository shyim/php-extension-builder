package composer

import (
	"testing"
)

func TestRejectsMissingOrInvalidType(t *testing.T) {
	_, err := ExtensionNameFromJSON([]byte(`{"type":"library"}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := `composer.json type must be "php-ext" or "php-ext-zend", but "library" was found.`
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestPrefersPhpExtExtensionNameAndStripsExtPrefix(t *testing.T) {
	name, err := ExtensionNameFromJSON([]byte(
		`{"type":"php-ext","php-ext":{"extension-name":"ext-test_ext"},"name":"vendor/ignored"}`,
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "test_ext" {
		t.Errorf("expected %q, got %q", "test_ext", name)
	}
}

func TestFallsBackToPackageNameSuffix(t *testing.T) {
	name, err := ExtensionNameFromJSON([]byte(`{"type":"php-ext-zend","name":"vendor/foo"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "foo" {
		t.Errorf("expected %q, got %q", "foo", name)
	}
}

func TestRejectsMissingExtensionAndPackageNames(t *testing.T) {
	_, err := ExtensionNameFromJSON([]byte(`{"type":"php-ext"}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := `Could not determine extension name: both ."php-ext"."extension-name" and .name are missing in composer.json`
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestRejectsInvalidExtensionName(t *testing.T) {
	_, err := ExtensionNameFromJSON([]byte(
		`{"type":"php-ext","php-ext":{"extension-name":"invalid-ext-name"}}`,
	))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	expected := `Invalid extension name: "invalid-ext-name" - must be alphanumeric/underscores only.`
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}
