package config

type ArtifactKind string

const (
	ArtifactZip ArtifactKind = "zip"
	ArtifactDeb ArtifactKind = "deb"
)

type TargetOs string

const (
	OsLinux  TargetOs = "linux"
	OsDarwin TargetOs = "darwin"
)

type Libc string

const (
	LibcGlibc   Libc = "glibc"
	LibcMusl    Libc = "musl"
	LibcBsdlibc Libc = "bsdlibc"
)

type BuildKind string

const (
	BuildKindC    BuildKind = "c"
	BuildKindRust BuildKind = "rust"
)

type BuildConfig struct {
	PackageVersion       string
	Artifacts            []ArtifactKind
	PhpVersion           string
	TargetOs             TargetOs
	Libc                 Libc
	Zts                  bool
	BuildPath            string
	ConfigureFlags       []string
	BeforePhpizeCommands []string
	AptPackages          []string
	ApkPackages          []string
	OutDir               string
	Image                string
	PhpConfig            string
	ExtensionKind        BuildKind
	CargoFeatures        []string
}
