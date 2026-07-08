package rustext

import (
	"testing"
)

func TestDetectsSimpleDependency(t *testing.T) {
	if !DeclaresExtPhpRs("[dependencies]\next-php-rs = \"0.12\"\n") {
		t.Error("expected true")
	}
}

func TestDetectsTableDependency(t *testing.T) {
	if !DeclaresExtPhpRs("[dependencies.ext-php-rs]\nversion = \"0.12\"\n") {
		t.Error("expected true")
	}
}

func TestDetectsWorkspaceDependency(t *testing.T) {
	if !DeclaresExtPhpRs("[dependencies]\next-php-rs.workspace = true\n") {
		t.Error("expected true")
	}
}

func TestIgnoresCommentedDependency(t *testing.T) {
	if DeclaresExtPhpRs("[dependencies]\n# ext-php-rs = \"0.12\"\n") {
		t.Error("expected false")
	}
}

func TestIgnoresUnrelatedManifest(t *testing.T) {
	if DeclaresExtPhpRs("[dependencies]\nserde = \"1\"\n") {
		t.Error("expected false")
	}
}

func TestPrefersLibNameOverPackageName(t *testing.T) {
	manifest := `[package]
name = "my-ext"

[lib]
name = "different_name"
crate-type = ["cdylib"]
`
	name := CdylibName(manifest)
	if name != "different_name" {
		t.Errorf("expected different_name, got %q", name)
	}
}

func TestFallsBackToPackageName(t *testing.T) {
	manifest := `[package]
name = "my-ext"
`
	name := CdylibName(manifest)
	if name != "my-ext" {
		t.Errorf("expected my-ext, got %q", name)
	}
}

func TestIgnoresNameInOtherTables(t *testing.T) {
	manifest := `[[bin]]
name = "helper"

[package]
name = "real_pkg"
`
	name := CdylibName(manifest)
	if name != "real_pkg" {
		t.Errorf("expected real_pkg, got %q", name)
	}
}

func TestReturnsNoneWithoutPackageOrLibName(t *testing.T) {
	manifest := `[workspace]
members = ["a"]
`
	name := CdylibName(manifest)
	if name != "" {
		t.Errorf("expected empty string, got %q", name)
	}
}
