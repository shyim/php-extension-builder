package rustext

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectsSimpleDependency(t *testing.T) {
	assert.True(t, DeclaresExtPhpRs("[dependencies]\next-php-rs = \"0.12\"\n"))
}

func TestDetectsTableDependency(t *testing.T) {
	assert.True(t, DeclaresExtPhpRs("[dependencies.ext-php-rs]\nversion = \"0.12\"\n"))
}

func TestDetectsWorkspaceDependency(t *testing.T) {
	assert.True(t, DeclaresExtPhpRs("[dependencies]\next-php-rs.workspace = true\n"))
}

func TestIgnoresCommentedDependency(t *testing.T) {
	assert.False(t, DeclaresExtPhpRs("[dependencies]\n# ext-php-rs = \"0.12\"\n"))
}

func TestIgnoresUnrelatedManifest(t *testing.T) {
	assert.False(t, DeclaresExtPhpRs("[dependencies]\nserde = \"1\"\n"))
}

func TestPrefersLibNameOverPackageName(t *testing.T) {
	manifest := `[package]
name = "my-ext"

[lib]
name = "different_name"
crate-type = ["cdylib"]
`
	assert.Equal(t, "different_name", CdylibName(manifest))
}

func TestFallsBackToPackageName(t *testing.T) {
	manifest := `[package]
name = "my-ext"
`
	assert.Equal(t, "my-ext", CdylibName(manifest))
}

func TestIgnoresNameInOtherTables(t *testing.T) {
	manifest := `[[bin]]
name = "helper"

[package]
name = "real_pkg"
`
	assert.Equal(t, "real_pkg", CdylibName(manifest))
}

func TestReturnsNoneWithoutPackageOrLibName(t *testing.T) {
	manifest := `[workspace]
members = ["a"]
`
	assert.Empty(t, CdylibName(manifest))
}
