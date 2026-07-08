package backend

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shyim/php-extension-builder/internal/config"
	"github.com/shyim/php-extension-builder/internal/packager"
)

type NativeDarwin struct{}

func (n NativeDarwin) Build(cfg *config.BuildConfig) (*BuildMetadata, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("darwin builds require running on a macOS host")
	}

	phpConfig := cfg.PhpConfig
	if phpConfig == "" {
		phpConfig = "php-config"
	}

	fmt.Fprintln(os.Stderr, "==> Building macOS extension natively")
	fmt.Fprintf(os.Stderr, "==> Build path: %s\n", cfg.BuildPath)
	fmt.Fprintf(os.Stderr, "==> php-config: %s\n", phpConfig)

	metadata, err := nativeMetadata(cfg, phpConfig)
	if err != nil {
		return nil, err
	}

	if err := ValidateRequestedMetadata(cfg, metadata); err != nil {
		return nil, err
	}

	for _, command := range cfg.BeforePhpizeCommands {
		if err := runNativeShell(command, cfg.BuildPath); err != nil {
			return nil, err
		}
	}

	switch cfg.ExtensionKind {
	case config.BuildKindC:
		if err := nativeBuildC(cfg); err != nil {
			return nil, err
		}
	case config.BuildKindRust:
		if err := nativeBuildRust(cfg); err != nil {
			return nil, err
		}
	}

	return metadata, nil
}

func nativeMetadata(cfg *config.BuildConfig, phpConfig string) (*BuildMetadata, error) {
	fmt.Fprintln(os.Stderr, "==> Detecting PHP build metadata")

	phpVersion, err := commandStdout(phpConfig, []string{"--version"}, cfg.BuildPath)
	if err != nil {
		return nil, fmt.Errorf("failed to run php-config --version: %w", err)
	}

	phpBinary, err := commandStdout(phpConfig, []string{"--php-binary"}, cfg.BuildPath)
	if err != nil {
		return nil, fmt.Errorf("failed to run php-config --php-binary: %w", err)
	}

	extensionDir, err := commandStdout(phpConfig, []string{"--extension-dir"}, cfg.BuildPath)
	if err != nil {
		return nil, fmt.Errorf("failed to run php-config --extension-dir: %w", err)
	}

	if phpBinary == "NONE" || phpBinary == "" {
		phpBinary = "php"
	}

	debugSuffix, err := commandStdout(phpBinary, []string{"-n", "-r", "echo PHP_DEBUG ? '-debug' : '';"}, cfg.BuildPath)
	if err != nil {
		return nil, fmt.Errorf("failed to detect PHP debug mode: %w", err)
	}

	ztsSuffix, err := commandStdout(phpBinary, []string{"-n", "-r", "echo ZEND_THREAD_SAFE ? '-zts' : '';"}, cfg.BuildPath)
	if err != nil {
		return nil, fmt.Errorf("failed to detect PHP ZTS mode: %w", err)
	}

	arch, err := commandStdout("uname", []string{"-m"}, cfg.BuildPath)
	if err != nil {
		return nil, fmt.Errorf("failed to detect architecture: %w", err)
	}

	normArch, err := normalizeArch(arch)
	if err != nil {
		return nil, err
	}

	return &BuildMetadata{
		PhpMajorMinor: packager.PhpMajorMinor(phpVersion),
		Arch:          normArch,
		ExtensionDir:  strings.TrimSpace(extensionDir),
		DebugSuffix:   strings.TrimSpace(debugSuffix),
		ZtsSuffix:     strings.TrimSpace(ztsSuffix),
	}, nil
}

func nativeBuildC(cfg *config.BuildConfig) error {
	if err := runNative("phpize", nil, cfg.BuildPath, "phpize"); err != nil {
		return err
	}

	flags := nativeConfigureFlags(cfg)
	if err := runNative("./configure", flags, cfg.BuildPath, "./configure"); err != nil {
		return err
	}

	return runNative("make", nil, cfg.BuildPath, "make")
}

func nativeBuildRust(cfg *config.BuildConfig) error {
	configM4 := filepath.Join(cfg.BuildPath, "config.m4")
	pieConfigM4 := filepath.Join(cfg.BuildPath, "pie", "config.m4")

	if fileExists(configM4) || fileExists(pieConfigM4) {
		if err := runNative("phpize", nil, cfg.BuildPath, "phpize"); err != nil {
			return err
		}

		flags := nativeConfigureFlags(cfg)
		if err := runNative("./configure", flags, cfg.BuildPath, "./configure"); err != nil {
			return err
		}

		return runNative("make", nil, cfg.BuildPath, "make")
	}

	args := []string{"build", "--release"}
	if len(cfg.CargoFeatures) > 0 {
		args = append(args, "--features", strings.Join(cfg.CargoFeatures, ","))
	}

	return runNative("cargo", args, cfg.BuildPath, "cargo build")
}

func nativeConfigureFlags(cfg *config.BuildConfig) []string {
	flags := make([]string, len(cfg.ConfigureFlags))
	copy(flags, cfg.ConfigureFlags)

	if cfg.PhpConfig != "" {
		hasPhpConfig := false
		for _, flag := range flags {
			if strings.HasPrefix(flag, "--with-php-config") {
				hasPhpConfig = true
				break
			}
		}
		if !hasPhpConfig {
			flags = append(flags, fmt.Sprintf("--with-php-config=%s", cfg.PhpConfig))
		}
	}

	return flags
}

func runNative(program string, args []string, cwd string, label string) error {
	cmd := exec.Command(program, args...)
	cmd.Dir = cwd
	_, _, err := runStreaming(cmd, label)
	return err
}

func runNativeShell(command string, cwd string) error {
	label := fmt.Sprintf("before phpize command `%s`", command)
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = cwd
	_, _, err := runStreaming(cmd, label)
	return err
}

func runStreaming(cmd *exec.Cmd, label string) ([]byte, []byte, error) {
	fmt.Fprintf(os.Stderr, "==> Running %s\n", label)

	var stdoutBuf, stderrBuf bytes.Buffer

	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	err := cmd.Run()
	if err != nil {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), fmt.Errorf("%s failed: %w", label, err)
	}

	return stdoutBuf.Bytes(), stderrBuf.Bytes(), nil
}

func commandStdout(program string, args []string, cwd string) (string, error) {
	cmd := exec.Command(program, args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w", program, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
