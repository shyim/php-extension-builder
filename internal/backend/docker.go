package backend

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shyim/php-extension-builder/internal/config"
	"github.com/shyim/php-extension-builder/internal/packager"
)

const metaPrefix = "__PIE_META_"

type DockerLinux struct{}

func (d DockerLinux) Build(cfg *config.BuildConfig) (*BuildMetadata, error) {
	image, err := dockerImage(cfg)
	if err != nil {
		return nil, err
	}

	workspace, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to determine current directory: %w", err)
	}

	workdir, err := dockerWorkdir(cfg.BuildPath)
	if err != nil {
		return nil, err
	}

	script := dockerScript(cfg)

	fmt.Fprintln(os.Stderr, "==> Building Linux extension in Docker")
	fmt.Fprintf(os.Stderr, "==> Docker image: %s\n", image)
	fmt.Fprintf(os.Stderr, "==> Workspace: %s\n", workspace)
	fmt.Fprintf(os.Stderr, "==> Container workdir: %s\n", workdir)

	args := []string{
		"run",
		"--rm",
		"-v", fmt.Sprintf("%s:/workspace", workspace),
		"-w", workdir,
		"-e", "HOST_UID",
		"-e", "HOST_GID",
	}

	cmd := exec.Command("docker")

	if uid, gid, ok := hostIds(); ok {
		args = append(args, "-e", fmt.Sprintf("HOST_UID=%s", uid), "-e", fmt.Sprintf("HOST_GID=%s", gid))
		cmd.Env = append(os.Environ(), fmt.Sprintf("HOST_UID=%s", uid), fmt.Sprintf("HOST_GID=%s", gid))
	} else {
		cmd.Env = os.Environ()
	}

	args = append(args, image, "sh", "-c", script)
	cmd.Args = append([]string{"docker"}, args...)

	stdout, _, err := runStreaming(cmd, "docker build")
	if err != nil {
		return nil, err
	}

	metadata, err := parseMetadata(stdout)
	if err != nil {
		return nil, err
	}

	if err := ValidateRequestedMetadata(cfg, metadata); err != nil {
		return nil, err
	}

	return metadata, nil
}

func hostIds() (string, string, bool) {
	uid, err := hostId("-u")
	if err != nil {
		return "", "", false
	}
	gid, err := hostId("-g")
	if err != nil {
		return "", "", false
	}
	return uid, gid, true
}

func hostId(arg string) (string, error) {
	cmd := exec.Command("id", arg)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func dockerImage(cfg *config.BuildConfig) (string, error) {
	if cfg.Image != "" {
		return cfg.Image, nil
	}

	if cfg.PhpVersion == "" {
		return "", fmt.Errorf("--php-version is required when --image is not supplied")
	}

	var suffix string
	switch cfg.Libc {
	case config.LibcGlibc:
		if cfg.Zts {
			suffix = "zts"
		} else {
			suffix = "cli"
		}
	case config.LibcMusl:
		if cfg.Zts {
			suffix = "zts-alpine"
		} else {
			suffix = "cli-alpine"
		}
	case config.LibcBsdlibc:
		return "", fmt.Errorf("bsdlibc is not a docker linux target")
	default:
		return "", fmt.Errorf("unknown libc: %s", cfg.Libc)
	}

	switch cfg.ExtensionKind {
	case config.BuildKindC:
		return fmt.Sprintf("php:%s-%s", cfg.PhpVersion, suffix), nil
	case config.BuildKindRust:
		return fmt.Sprintf("ghcr.io/shyim/php-extension-builder-rust:%s-%s", cfg.PhpVersion, suffix), nil
	default:
		return "", fmt.Errorf("unknown build kind: %s", cfg.ExtensionKind)
	}
}

func dockerWorkdir(buildPath string) (string, error) {
	if filepath.IsAbs(buildPath) {
		return "", fmt.Errorf("--build-path must be relative for docker builds")
	}

	clean := filepath.Clean(buildPath)
	if clean == "." || clean == "" {
		return "/workspace", nil
	}

	clean = filepath.ToSlash(clean)
	clean = strings.TrimPrefix(clean, "./")
	clean = strings.TrimSuffix(clean, "/")

	return fmt.Sprintf("/workspace/%s", clean), nil
}

func dockerScript(cfg *config.BuildConfig) string {
	switch cfg.ExtensionKind {
	case config.BuildKindC:
		return dockerScriptC(cfg)
	case config.BuildKindRust:
		return dockerScriptRust(cfg)
	default:
		return ""
	}
}

func dockerScriptC(cfg *config.BuildConfig) string {
	beforePhpize := dockerBeforePhpizeCommands(cfg.BeforePhpizeCommands)
	configure := "./configure"
	if len(cfg.ConfigureFlags) > 0 {
		configure = fmt.Sprintf("./configure %s", shellArgs(cfg.ConfigureFlags))
	}
	debianPkgArgs := shellArgs(cfg.AptPackages)
	alpinePkgArgs := shellArgs(cfg.ApkPackages)

	return fmt.Sprintf(
		"set -eu\n"+
			"echo \"==> Installing build dependencies\" >&2\n"+
			"if command -v apk >/dev/null 2>&1; then\n"+
			"  apk add --no-cache ${PHPIZE_DEPS:-autoconf dpkg-dev dpkg file g++ gcc libc-dev make pkgconf re2c} %s\n"+
			"elif command -v apt-get >/dev/null 2>&1; then\n"+
			"  apt-get update\n"+
			"  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ${PHPIZE_DEPS:-autoconf dpkg-dev file g++ gcc libc-dev make pkg-config re2c} %s\n"+
			"  rm -rf /var/lib/apt/lists/*\n"+
			"fi\n"+
			"%s"+
			"echo \"==> Running phpize\" >&2\n"+
			"phpize\n"+
			"echo \"==> Running configure\" >&2\n"+
			"%s\n"+
			"echo \"==> Running make\" >&2\n"+
			"make\n"+
			"%s",
		alpinePkgArgs, debianPkgArgs, beforePhpize, configure, dockerMetadataTail(),
	)
}

func dockerScriptRust(cfg *config.BuildConfig) string {
	beforePhpize := dockerBeforePhpizeCommands(cfg.BeforePhpizeCommands)
	configure := "./configure"
	if len(cfg.ConfigureFlags) > 0 {
		configure = fmt.Sprintf("./configure %s", shellArgs(cfg.ConfigureFlags))
	}
	cargoBuild := "cargo build --release"
	if len(cfg.CargoFeatures) > 0 {
		cargoBuild = fmt.Sprintf("cargo build --release --features %s", shellQuote(strings.Join(cfg.CargoFeatures, ",")))
	}
	debianPkgArgs := shellArgs(cfg.AptPackages)
	alpinePkgArgs := shellArgs(cfg.ApkPackages)

	return fmt.Sprintf(
		"set -eu\n"+
			"echo \"==> Installing build dependencies\" >&2\n"+
			"if command -v apk >/dev/null 2>&1; then\n"+
			"  apk add --no-cache ${PHPIZE_DEPS:-autoconf dpkg-dev dpkg file g++ gcc libc-dev make pkgconf re2c} clang clang-dev llvm-dev %s\n"+
			"elif command -v apt-get >/dev/null 2>&1; then\n"+
			"  apt-get update\n"+
			"  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ${PHPIZE_DEPS:-autoconf dpkg-dev file g++ gcc libc-dev make pkg-config re2c} clang libclang-dev %s\n"+
			"  rm -rf /var/lib/apt/lists/*\n"+
			"fi\n"+
			"if ! command -v cargo >/dev/null 2>&1; then\n"+
			"  echo \"error: cargo not found; use a Rust-enabled image such as ghcr.io/shyim/php-extension-builder-rust\" >&2\n"+
			"  exit 1\n"+
			"fi\n"+
			"if ! ls \"${LIBCLANG_PATH:-/nonexistent}\"/libclang*.so* >/dev/null 2>&1; then\n"+
			"  if command -v llvm-config >/dev/null 2>&1; then\n"+
			"    LIBCLANG_PATH=\"$(llvm-config --libdir)\"\n"+
			"  else\n"+
			"    LIBCLANG_PATH=\"$(dirname \"$(find / -name 'libclang*.so*' -print 2>/dev/null | head -n1)\")\"\n"+
			"  fi\n"+
			"  export LIBCLANG_PATH\n"+
			"fi\n"+
			"%s"+
			"if [ -f config.m4 ] || [ -f pie/config.m4 ]; then\n"+
			"  echo \"==> Detected PIE build files (phpize mode)\" >&2\n"+
			"  phpize\n"+
			"  %s\n"+
			"  make\n"+
			"else\n"+
			"  echo \"==> No PIE build files (cargo mode)\" >&2\n"+
			"  %s\n"+
			"fi\n"+
			"%s",
		alpinePkgArgs, debianPkgArgs, beforePhpize, configure, cargoBuild, dockerMetadataTail(),
	)
}

func dockerMetadataTail() string {
	return fmt.Sprintf(
		"echo \"==> Collecting build metadata\" >&2\n"+
			"php_binary=\"$(php-config --php-binary)\"\n"+
			"if [ \"$php_binary\" = \"NONE\" ]; then\n"+
			"  php_binary=php\n"+
			"fi\n"+
			"printf '%sPHP_VERSION=%%s\\n' \"$(php-config --version)\"\n"+
			"printf '%sARCH=%%s\\n' \"$(uname -m)\"\n"+
			"printf '%sEXTENSION_DIR=%%s\\n' \"$(php-config --extension-dir)\"\n"+
			"printf '%sDEBUG=%%s\\n' \"$(\"$php_binary\" -n -r \"echo PHP_DEBUG ? '-debug' : '';\")\"\n"+
			"printf '%sZTS=%%s\\n' \"$(\"$php_binary\" -n -r \"echo ZEND_THREAD_SAFE ? '-zts' : '';\")\"\n"+
			"if [ -n \"${HOST_UID:-}\" ] && [ -n \"${HOST_GID:-}\" ]; then\n"+
			"  echo \"==> Restoring file ownership\" >&2\n"+
			"  chown -R \"$HOST_UID:$HOST_GID\" .\n"+
			"fi\n",
		metaPrefix, metaPrefix, metaPrefix, metaPrefix, metaPrefix,
	)
}

func dockerBeforePhpizeCommands(commands []string) string {
	var parts []string
	for _, cmd := range commands {
		msg := fmt.Sprintf("==> Running before phpize command: %s", cmd)
		parts = append(parts, fmt.Sprintf("echo %s >&2\n%s", shellQuote(msg), cmd))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n") + "\n"
}

func shellArgs(values []string) string {
	var quoted []string
	for _, val := range values {
		quoted = append(quoted, shellQuote(val))
	}
	return strings.Join(quoted, " ")
}

func parseMetadata(stdoutBytes []byte) (*BuildMetadata, error) {
	stdout := string(stdoutBytes)
	var phpVersion, arch, extensionDir, debugSuffix, ztsSuffix string
	var hasPhpVersion, hasArch, hasExtensionDir bool

	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, metaPrefix+"PHP_VERSION=") {
			phpVersion = strings.TrimPrefix(line, metaPrefix+"PHP_VERSION=")
			hasPhpVersion = true
		} else if strings.HasPrefix(line, metaPrefix+"ARCH=") {
			arch = strings.TrimPrefix(line, metaPrefix+"ARCH=")
			hasArch = true
		} else if strings.HasPrefix(line, metaPrefix+"EXTENSION_DIR=") {
			extensionDir = strings.TrimPrefix(line, metaPrefix+"EXTENSION_DIR=")
			hasExtensionDir = true
		} else if strings.HasPrefix(line, metaPrefix+"DEBUG=") {
			debugSuffix = strings.TrimPrefix(line, metaPrefix+"DEBUG=")
		} else if strings.HasPrefix(line, metaPrefix+"ZTS=") {
			ztsSuffix = strings.TrimPrefix(line, metaPrefix+"ZTS=")
		}
	}

	if !hasPhpVersion {
		return nil, fmt.Errorf("docker build did not report PHP version")
	}
	if !hasArch {
		return nil, fmt.Errorf("docker build did not report architecture")
	}
	if !hasExtensionDir {
		return nil, fmt.Errorf("docker build did not report extension directory")
	}

	normArch, err := normalizeArch(arch)
	if err != nil {
		return nil, err
	}

	return &BuildMetadata{
		PhpMajorMinor: packager.PhpMajorMinor(phpVersion),
		Arch:          normArch,
		ExtensionDir:  extensionDir,
		DebugSuffix:   debugSuffix,
		ZtsSuffix:     ztsSuffix,
	}, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
