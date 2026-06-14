use crate::BuildConfig;
use crate::cli::{BuildKind, Libc};
use crate::package::php_major_minor;
use anyhow::{Context, Result, anyhow, bail};
use std::io::{self, Read, Write};
use std::path::{Path, PathBuf};
use std::process::{Command, Output, Stdio};
use std::thread::{self, JoinHandle};

const META_PREFIX: &str = "__PIE_META_";

#[derive(Debug, Clone, Eq, PartialEq)]
pub struct BuildMetadata {
    pub php_major_minor: String,
    pub arch: String,
    pub extension_dir: String,
    pub debug_suffix: String,
    pub zts_suffix: String,
}

pub trait BuildBackend {
    fn build(&self, config: &BuildConfig) -> Result<BuildMetadata>;
}

pub struct DockerLinux;
pub struct NativeDarwin;

impl BuildBackend for DockerLinux {
    fn build(&self, config: &BuildConfig) -> Result<BuildMetadata> {
        let image = docker_image(config)?;
        let workspace = std::env::current_dir().context("failed to determine current directory")?;
        let workdir = docker_workdir(&config.build_path)?;
        let script = docker_script(config);

        eprintln!("==> Building Linux extension in Docker");
        eprintln!("==> Docker image: {image}");
        eprintln!("==> Workspace: {}", workspace.display());
        eprintln!("==> Container workdir: {workdir}");

        let mut command = Command::new("docker");
        command
            .arg("run")
            .arg("--rm")
            .arg("-v")
            .arg(format!("{}:/workspace", workspace.display()))
            .arg("-w")
            .arg(workdir)
            .arg("-e")
            .arg("HOST_UID")
            .arg("-e")
            .arg("HOST_GID");

        if let Some((uid, gid)) = host_ids() {
            command.env("HOST_UID", uid);
            command.env("HOST_GID", gid);
        }

        command.arg(image).arg("sh").arg("-c").arg(script);

        let output = run_streaming(&mut command, "docker build")?;
        ensure_success(&output, "docker build")?;
        let metadata = parse_metadata(&output)?;
        validate_requested_metadata(config, &metadata)?;
        Ok(metadata)
    }
}

impl BuildBackend for NativeDarwin {
    fn build(&self, config: &BuildConfig) -> Result<BuildMetadata> {
        if !cfg!(target_os = "macos") {
            bail!("darwin builds require running on a macOS host");
        }

        let php_config = config
            .php_config
            .as_deref()
            .unwrap_or_else(|| Path::new("php-config"));
        eprintln!("==> Building macOS extension natively");
        eprintln!("==> Build path: {}", config.build_path.display());
        eprintln!("==> php-config: {}", php_config.display());
        let metadata = native_metadata(config, php_config)?;
        validate_requested_metadata(config, &metadata)?;

        for command in &config.before_phpize_commands {
            run_native_shell(command, &config.build_path)?;
        }

        match config.extension_kind {
            BuildKind::C => native_build_c(config)?,
            BuildKind::Rust => native_build_rust(config)?,
        }

        Ok(metadata)
    }
}

fn native_build_c(config: &BuildConfig) -> Result<()> {
    run_native(
        CommandSpec::new("phpize", &[], &config.build_path),
        "phpize",
    )?;
    run_native(
        CommandSpec::new(
            "./configure",
            &native_configure_flags(config),
            &config.build_path,
        ),
        "./configure",
    )?;
    run_native(CommandSpec::new("make", &[], &config.build_path), "make")
}

fn native_build_rust(config: &BuildConfig) -> Result<()> {
    if config.build_path.join("config.m4").is_file()
        || config.build_path.join("pie").join("config.m4").is_file()
    {
        run_native(
            CommandSpec::new("phpize", &[], &config.build_path),
            "phpize",
        )?;
        run_native(
            CommandSpec::new(
                "./configure",
                &native_configure_flags(config),
                &config.build_path,
            ),
            "./configure",
        )?;
        run_native(CommandSpec::new("make", &[], &config.build_path), "make")
    } else {
        let mut args = vec!["build".to_string(), "--release".to_string()];
        if !config.cargo_features.is_empty() {
            args.push("--features".to_string());
            args.push(config.cargo_features.join(","));
        }
        run_native(
            CommandSpec::new("cargo", &args, &config.build_path),
            "cargo build",
        )
    }
}

fn native_configure_flags(config: &BuildConfig) -> Vec<String> {
    let mut configure_flags = config.configure_flags.clone();
    if let Some(php_config_path) = &config.php_config
        && !configure_flags
            .iter()
            .any(|flag| flag.starts_with("--with-php-config"))
    {
        configure_flags.push(format!("--with-php-config={}", php_config_path.display()));
    }

    configure_flags
}

fn native_metadata(config: &BuildConfig, php_config: &Path) -> Result<BuildMetadata> {
    eprintln!("==> Detecting PHP build metadata");
    let php_version = command_stdout(php_config, &["--version"], &config.build_path)
        .context("failed to run php-config --version")?;
    let php_binary = command_stdout(php_config, &["--php-binary"], &config.build_path)
        .context("failed to run php-config --php-binary")?;
    let extension_dir = command_stdout(php_config, &["--extension-dir"], &config.build_path)
        .context("failed to run php-config --extension-dir")?;
    let php_binary = if php_binary.trim() == "NONE" {
        PathBuf::from("php")
    } else {
        PathBuf::from(php_binary.trim())
    };

    let debug_suffix = command_stdout(
        &php_binary,
        &["-n", "-r", "echo PHP_DEBUG ? '-debug' : '';"],
        &config.build_path,
    )
    .context("failed to detect PHP debug mode")?;
    let zts_suffix = command_stdout(
        &php_binary,
        &["-n", "-r", "echo ZEND_THREAD_SAFE ? '-zts' : '';"],
        &config.build_path,
    )
    .context("failed to detect PHP ZTS mode")?;
    let arch = command_stdout(Path::new("uname"), &["-m"], &config.build_path)
        .context("failed to detect architecture")?;

    Ok(BuildMetadata {
        php_major_minor: php_major_minor(&php_version),
        arch: normalize_arch(&arch)?,
        extension_dir: extension_dir.trim().to_string(),
        debug_suffix: debug_suffix.trim().to_string(),
        zts_suffix: zts_suffix.trim().to_string(),
    })
}

struct CommandSpec<'a> {
    program: &'a str,
    args: Vec<String>,
    cwd: &'a Path,
}

impl<'a> CommandSpec<'a> {
    fn new(program: &'a str, args: &[String], cwd: &'a Path) -> Self {
        Self {
            program,
            args: args.to_vec(),
            cwd,
        }
    }
}

fn run_native(spec: CommandSpec<'_>, label: &str) -> Result<()> {
    let mut command = Command::new(spec.program);
    command.args(spec.args).current_dir(spec.cwd);

    let output = run_streaming(&mut command, label)?;
    ensure_success(&output, label)
}

fn run_native_shell(command: &str, cwd: &Path) -> Result<()> {
    let label = format!("before phpize command `{command}`");
    let mut shell = Command::new("sh");
    shell.arg("-c").arg(command).current_dir(cwd);

    let output = run_streaming(&mut shell, &label)?;
    ensure_success(&output, &label)
}

fn run_streaming(command: &mut Command, label: &str) -> Result<Output> {
    eprintln!("==> Running {label}");
    let mut child = command
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .with_context(|| format!("failed to start {label}"))?;

    let stdout = child.stdout.take().context("failed to capture stdout")?;
    let stderr = child.stderr.take().context("failed to capture stderr")?;
    let stdout_thread = stream_output(stdout, StreamTarget::Stdout);
    let stderr_thread = stream_output(stderr, StreamTarget::Stderr);
    let status = child
        .wait()
        .with_context(|| format!("failed to wait for {label}"))?;
    let stdout = join_stream(stdout_thread, label, "stdout")?;
    let stderr = join_stream(stderr_thread, label, "stderr")?;

    Ok(Output {
        status,
        stdout,
        stderr,
    })
}

enum StreamTarget {
    Stdout,
    Stderr,
}

fn stream_output<R>(mut reader: R, target: StreamTarget) -> JoinHandle<io::Result<Vec<u8>>>
where
    R: Read + Send + 'static,
{
    thread::spawn(move || {
        let mut captured = Vec::new();
        let mut buffer = [0; 8192];

        loop {
            let bytes_read = reader.read(&mut buffer)?;
            if bytes_read == 0 {
                break;
            }

            let bytes = &buffer[..bytes_read];
            captured.extend_from_slice(bytes);
            match target {
                StreamTarget::Stdout => {
                    let mut stdout = io::stdout().lock();
                    stdout.write_all(bytes)?;
                    stdout.flush()?;
                }
                StreamTarget::Stderr => {
                    let mut stderr = io::stderr().lock();
                    stderr.write_all(bytes)?;
                    stderr.flush()?;
                }
            }
        }

        Ok(captured)
    })
}

fn join_stream(
    thread: JoinHandle<io::Result<Vec<u8>>>,
    label: &str,
    stream_name: &str,
) -> Result<Vec<u8>> {
    thread
        .join()
        .map_err(|_| anyhow!("{label} {stream_name} stream thread panicked"))?
        .with_context(|| format!("failed to stream {label} {stream_name}"))
}

fn command_stdout(program: &Path, args: &[&str], cwd: &Path) -> Result<String> {
    let output = Command::new(program)
        .args(args)
        .current_dir(cwd)
        .output()
        .with_context(|| format!("failed to start {}", program.display()))?;

    ensure_success(&output, &program.display().to_string())?;
    Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
}

fn ensure_success(output: &Output, label: &str) -> Result<()> {
    if output.status.success() {
        return Ok(());
    }

    Err(anyhow!("{label} failed with status {}", output.status))
}

fn validate_requested_metadata(config: &BuildConfig, metadata: &BuildMetadata) -> Result<()> {
    if let Some(requested_php_version) = &config.php_version {
        let requested_php_major_minor = php_major_minor(requested_php_version);
        if requested_php_major_minor != metadata.php_major_minor {
            bail!(
                "requested PHP {requested_php_major_minor}, but selected PHP reports {}",
                metadata.php_major_minor
            );
        }
    }

    let actual_zts = metadata.zts_suffix == "-zts";
    match (config.zts, actual_zts) {
        (true, false) => bail!("--zts was requested, but selected PHP is non-ZTS"),
        (false, true) => bail!("non-ZTS build was requested, but selected PHP is ZTS; pass --zts"),
        _ => Ok(()),
    }
}

fn docker_image(config: &BuildConfig) -> Result<String> {
    if let Some(image) = &config.image {
        return Ok(image.clone());
    }

    let php_version = config
        .php_version
        .as_deref()
        .context("--php-version is required when --image is not supplied")?;

    let suffix = match (config.libc, config.zts) {
        (Libc::Glibc, false) => "cli",
        (Libc::Glibc, true) => "zts",
        (Libc::Musl, false) => "cli-alpine",
        (Libc::Musl, true) => "zts-alpine",
        (Libc::Bsdlibc, _) => bail!("bsdlibc is not a docker linux target"),
    };

    Ok(match config.extension_kind {
        BuildKind::C => format!("php:{php_version}-{suffix}"),
        BuildKind::Rust => {
            format!("ghcr.io/shyim/php-extension-builder-rust:{php_version}-{suffix}")
        }
    })
}

fn docker_workdir(build_path: &Path) -> Result<String> {
    if build_path.is_absolute() {
        bail!("--build-path must be relative for docker builds");
    }

    if build_path == Path::new(".") {
        return Ok("/workspace".to_string());
    }

    Ok(format!(
        "/workspace/{}",
        build_path
            .to_string_lossy()
            .trim_start_matches("./")
            .trim_end_matches('/')
    ))
}

fn docker_script(config: &BuildConfig) -> String {
    match config.extension_kind {
        BuildKind::C => docker_script_c(config),
        BuildKind::Rust => docker_script_rust(config),
    }
}

fn docker_script_c(config: &BuildConfig) -> String {
    let before_phpize_commands = docker_before_phpize_commands(&config.before_phpize_commands);
    let configure = if config.configure_flags.is_empty() {
        "./configure".to_string()
    } else {
        format!(
            "./configure {}",
            config
                .configure_flags
                .iter()
                .map(|flag| shell_quote(flag))
                .collect::<Vec<_>>()
                .join(" ")
        )
    };
    let debian_package_args = shell_args(&config.apt_packages);
    let alpine_package_args = shell_args(&config.apk_packages);

    format!(
        r#"set -eu
echo "==> Installing build dependencies" >&2
if command -v apk >/dev/null 2>&1; then
  apk add --no-cache ${{PHPIZE_DEPS:-autoconf dpkg-dev dpkg file g++ gcc libc-dev make pkgconf re2c}} {alpine_package_args}
elif command -v apt-get >/dev/null 2>&1; then
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ${{PHPIZE_DEPS:-autoconf dpkg-dev file g++ gcc libc-dev make pkg-config re2c}} {debian_package_args}
  rm -rf /var/lib/apt/lists/*
fi
{before_phpize_commands}echo "==> Running phpize" >&2
phpize
echo "==> Running configure" >&2
{configure}
echo "==> Running make" >&2
make
{metadata_tail}"#,
        metadata_tail = docker_metadata_tail()
    )
}

fn docker_script_rust(config: &BuildConfig) -> String {
    let before_phpize_commands = docker_before_phpize_commands(&config.before_phpize_commands);
    let configure = if config.configure_flags.is_empty() {
        "./configure".to_string()
    } else {
        format!(
            "./configure {}",
            config
                .configure_flags
                .iter()
                .map(|flag| shell_quote(flag))
                .collect::<Vec<_>>()
                .join(" ")
        )
    };
    let cargo_build = if config.cargo_features.is_empty() {
        "cargo build --release".to_string()
    } else {
        format!(
            "cargo build --release --features {}",
            shell_quote(&config.cargo_features.join(","))
        )
    };
    let debian_package_args = shell_args(&config.apt_packages);
    let alpine_package_args = shell_args(&config.apk_packages);

    format!(
        r#"set -eu
echo "==> Installing build dependencies" >&2
if command -v apk >/dev/null 2>&1; then
  apk add --no-cache ${{PHPIZE_DEPS:-autoconf dpkg-dev dpkg file g++ gcc libc-dev make pkgconf re2c}} clang clang-dev llvm-dev {alpine_package_args}
elif command -v apt-get >/dev/null 2>&1; then
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ${{PHPIZE_DEPS:-autoconf dpkg-dev file g++ gcc libc-dev make pkg-config re2c}} clang libclang-dev {debian_package_args}
  rm -rf /var/lib/apt/lists/*
fi
if ! command -v cargo >/dev/null 2>&1; then
  echo "error: cargo not found; use a Rust-enabled image such as ghcr.io/shyim/php-extension-builder-rust" >&2
  exit 1
fi
if [ -z "${{LIBCLANG_PATH:-}}" ] && command -v llvm-config >/dev/null 2>&1; then
  LIBCLANG_PATH="$(llvm-config --libdir)"
  export LIBCLANG_PATH
fi
{before_phpize_commands}if [ -f config.m4 ] || [ -f pie/config.m4 ]; then
  echo "==> Detected PIE build files (phpize mode)" >&2
  phpize
  {configure}
  make
else
  echo "==> No PIE build files (cargo mode)" >&2
  {cargo_build}
fi
{metadata_tail}"#,
        metadata_tail = docker_metadata_tail()
    )
}

fn docker_metadata_tail() -> String {
    format!(
        r#"echo "==> Collecting build metadata" >&2
php_binary="$(php-config --php-binary)"
if [ "$php_binary" = "NONE" ]; then
  php_binary=php
fi
printf '{META_PREFIX}PHP_VERSION=%s\n' "$(php-config --version)"
printf '{META_PREFIX}ARCH=%s\n' "$(uname -m)"
printf '{META_PREFIX}EXTENSION_DIR=%s\n' "$(php-config --extension-dir)"
printf '{META_PREFIX}DEBUG=%s\n' "$("$php_binary" -n -r "echo PHP_DEBUG ? '-debug' : '';")"
printf '{META_PREFIX}ZTS=%s\n' "$("$php_binary" -n -r "echo ZEND_THREAD_SAFE ? '-zts' : '';")"
if [ -n "${{HOST_UID:-}}" ] && [ -n "${{HOST_GID:-}}" ]; then
  echo "==> Restoring file ownership" >&2
  chown -R "$HOST_UID:$HOST_GID" .
fi
"#
    )
}

fn docker_before_phpize_commands(commands: &[String]) -> String {
    let script = commands
        .iter()
        .map(|command| {
            format!(
                "echo {} >&2\n{command}",
                shell_quote(&format!("==> Running before phpize command: {command}"))
            )
        })
        .collect::<Vec<_>>()
        .join("\n");

    if script.is_empty() {
        script
    } else {
        format!("{script}\n")
    }
}

fn shell_args(values: &[String]) -> String {
    values
        .iter()
        .map(|value| shell_quote(value))
        .collect::<Vec<_>>()
        .join(" ")
}

fn shell_quote(value: &str) -> String {
    format!("'{}'", value.replace('\'', "'\\''"))
}

fn parse_metadata(output: &Output) -> Result<BuildMetadata> {
    let stdout = String::from_utf8_lossy(&output.stdout);
    let mut php_version = None;
    let mut arch = None;
    let mut extension_dir = None;
    let mut debug_suffix = None;
    let mut zts_suffix = None;

    for line in stdout.lines() {
        if let Some(value) = line.strip_prefix(&format!("{META_PREFIX}PHP_VERSION=")) {
            php_version = Some(value.to_string());
        } else if let Some(value) = line.strip_prefix(&format!("{META_PREFIX}ARCH=")) {
            arch = Some(value.to_string());
        } else if let Some(value) = line.strip_prefix(&format!("{META_PREFIX}EXTENSION_DIR=")) {
            extension_dir = Some(value.to_string());
        } else if let Some(value) = line.strip_prefix(&format!("{META_PREFIX}DEBUG=")) {
            debug_suffix = Some(value.to_string());
        } else if let Some(value) = line.strip_prefix(&format!("{META_PREFIX}ZTS=")) {
            zts_suffix = Some(value.to_string());
        }
    }

    Ok(BuildMetadata {
        php_major_minor: php_major_minor(
            &php_version.context("docker build did not report PHP version")?,
        ),
        arch: normalize_arch(&arch.context("docker build did not report architecture")?)?,
        extension_dir: extension_dir.context("docker build did not report extension directory")?,
        debug_suffix: debug_suffix.unwrap_or_default(),
        zts_suffix: zts_suffix.unwrap_or_default(),
    })
}

fn normalize_arch(value: &str) -> Result<String> {
    match value.trim() {
        "x86_64" | "amd64" => Ok("x86_64".to_string()),
        "aarch64" | "arm64" => Ok("arm64".to_string()),
        "i386" | "i686" | "x86" => Ok("x86".to_string()),
        other => bail!("unsupported architecture: {other}"),
    }
}

fn host_ids() -> Option<(String, String)> {
    let uid = host_id("-u")?;
    let gid = host_id("-g")?;
    Some((uid, gid))
}

fn host_id(arg: &str) -> Option<String> {
    let output = Command::new("id").arg(arg).output().ok()?;
    if !output.status.success() {
        return None;
    }

    Some(String::from_utf8_lossy(&output.stdout).trim().to_string())
}

#[cfg(test)]
mod tests {
    use super::{
        BuildMetadata, docker_script, docker_workdir, normalize_arch, validate_requested_metadata,
    };
    use crate::BuildConfig;
    use crate::backend::docker_image;
    use crate::cli::{ArtifactKind, BuildKind, Libc, TargetOs};
    use std::path::Path;
    use std::path::PathBuf;

    fn linux_config(libc: Libc, zts: bool) -> BuildConfig {
        BuildConfig {
            package_version: "1.2.3".to_string(),
            artifacts: vec![ArtifactKind::Zip],
            php_version: Some("8.3".to_string()),
            target_os: TargetOs::Linux,
            libc,
            zts,
            build_path: PathBuf::from("."),
            configure_flags: Vec::new(),
            before_phpize_commands: Vec::new(),
            apt_packages: Vec::new(),
            apk_packages: Vec::new(),
            out_dir: PathBuf::from("."),
            image: None,
            php_config: None,
            extension_kind: BuildKind::C,
            cargo_features: Vec::new(),
        }
    }

    #[test]
    fn selects_official_php_images() {
        assert_eq!(
            docker_image(&linux_config(Libc::Glibc, false)).unwrap(),
            "php:8.3-cli"
        );
        assert_eq!(
            docker_image(&linux_config(Libc::Glibc, true)).unwrap(),
            "php:8.3-zts"
        );
        assert_eq!(
            docker_image(&linux_config(Libc::Musl, false)).unwrap(),
            "php:8.3-cli-alpine"
        );
        assert_eq!(
            docker_image(&linux_config(Libc::Musl, true)).unwrap(),
            "php:8.3-zts-alpine"
        );
    }

    #[test]
    fn selects_rust_ghcr_images() {
        let mut config = linux_config(Libc::Glibc, false);
        config.extension_kind = BuildKind::Rust;
        assert_eq!(
            docker_image(&config).unwrap(),
            "ghcr.io/shyim/php-extension-builder-rust:8.3-cli"
        );

        let mut config = linux_config(Libc::Musl, true);
        config.extension_kind = BuildKind::Rust;
        assert_eq!(
            docker_image(&config).unwrap(),
            "ghcr.io/shyim/php-extension-builder-rust:8.3-zts-alpine"
        );
    }

    #[test]
    fn rust_image_respects_explicit_override() {
        let mut config = linux_config(Libc::Glibc, false);
        config.extension_kind = BuildKind::Rust;
        config.image = Some("ghcr.io/acme/custom:8.3".to_string());

        assert_eq!(docker_image(&config).unwrap(), "ghcr.io/acme/custom:8.3");
    }

    #[test]
    fn rust_script_branches_on_pie_and_runs_cargo() {
        let mut config = linux_config(Libc::Glibc, false);
        config.extension_kind = BuildKind::Rust;
        config.cargo_features = vec!["closure".to_string(), "anyhow".to_string()];
        let script = docker_script(&config);

        assert!(script.contains("if [ -f config.m4 ] || [ -f pie/config.m4 ]; then"));
        assert!(script.contains("cargo build --release --features 'closure,anyhow'"));
        assert!(script.contains("clang"));
    }

    #[test]
    fn rust_script_without_features_runs_plain_cargo() {
        let mut config = linux_config(Libc::Glibc, false);
        config.extension_kind = BuildKind::Rust;
        let script = docker_script(&config);

        assert!(script.contains("\n  cargo build --release\n"));
    }

    #[test]
    fn docker_workdir_is_under_workspace() {
        assert_eq!(docker_workdir(Path::new(".")).unwrap(), "/workspace");
        assert_eq!(
            docker_workdir(Path::new("src/php/ext/grpc")).unwrap(),
            "/workspace/src/php/ext/grpc"
        );
    }

    #[test]
    fn docker_workdir_rejects_absolute_paths() {
        assert!(docker_workdir(Path::new("/tmp/ext")).is_err());
    }

    #[test]
    fn docker_script_quotes_configure_flags() {
        let mut config = linux_config(Libc::Glibc, false);
        config.configure_flags = vec![
            "--enable-test".to_string(),
            "--with-name=O'Hara".to_string(),
        ];
        let script = docker_script(&config);

        assert!(script.contains("./configure '--enable-test' '--with-name=O'\\''Hara'"));
    }

    #[test]
    fn docker_script_adds_custom_distro_packages() {
        let mut config = linux_config(Libc::Glibc, false);
        config.apt_packages = vec!["libzstd-dev".to_string(), "libfoo=1.2".to_string()];
        config.apk_packages = vec!["zstd-dev".to_string(), "foo-dev".to_string()];
        let script = docker_script(&config);

        assert!(script.contains("apk add --no-cache ${PHPIZE_DEPS:-autoconf dpkg-dev dpkg file g++ gcc libc-dev make pkgconf re2c} 'zstd-dev' 'foo-dev'"));
        assert!(script.contains("apt-get install -y --no-install-recommends ${PHPIZE_DEPS:-autoconf dpkg-dev file g++ gcc libc-dev make pkg-config re2c} 'libzstd-dev' 'libfoo=1.2'"));
    }

    #[test]
    fn docker_script_runs_before_phpize_commands() {
        let mut config = linux_config(Libc::Glibc, false);
        config.before_phpize_commands = vec![
            "composer install --no-dev".to_string(),
            "./autogen.sh --force".to_string(),
        ];
        let script = docker_script(&config);

        assert!(script.contains(
            "echo '==> Running before phpize command: composer install --no-dev' >&2\ncomposer install --no-dev"
        ));
        assert!(script.contains(
            "echo '==> Running before phpize command: ./autogen.sh --force' >&2\n./autogen.sh --force"
        ));
        assert!(script.contains("./autogen.sh --force\necho \"==> Running phpize\" >&2\nphpize"));
    }

    #[test]
    fn normalizes_architecture_names() {
        assert_eq!(normalize_arch("x86_64").unwrap(), "x86_64");
        assert_eq!(normalize_arch("amd64").unwrap(), "x86_64");
        assert_eq!(normalize_arch("aarch64").unwrap(), "arm64");
        assert_eq!(normalize_arch("arm64").unwrap(), "arm64");
        assert_eq!(normalize_arch("i686").unwrap(), "x86");
    }

    #[test]
    fn validates_requested_php_version() {
        let config = linux_config(Libc::Glibc, false);
        let metadata = BuildMetadata {
            php_major_minor: "8.2".to_string(),
            arch: "arm64".to_string(),
            extension_dir: "/usr/local/lib/php/extensions/no-debug-non-zts-20230831".to_string(),
            debug_suffix: String::new(),
            zts_suffix: String::new(),
        };

        let error = validate_requested_metadata(&config, &metadata).unwrap_err();
        assert_eq!(
            error.to_string(),
            "requested PHP 8.3, but selected PHP reports 8.2"
        );
    }

    #[test]
    fn validates_requested_zts_mode() {
        let config = linux_config(Libc::Glibc, true);
        let metadata = BuildMetadata {
            php_major_minor: "8.3".to_string(),
            arch: "arm64".to_string(),
            extension_dir: "/usr/local/lib/php/extensions/no-debug-non-zts-20230831".to_string(),
            debug_suffix: String::new(),
            zts_suffix: String::new(),
        };

        let error = validate_requested_metadata(&config, &metadata).unwrap_err();
        assert_eq!(
            error.to_string(),
            "--zts was requested, but selected PHP is non-ZTS"
        );
    }

    #[test]
    fn validates_implicit_nts_mode() {
        let config = linux_config(Libc::Glibc, false);
        let metadata = BuildMetadata {
            php_major_minor: "8.3".to_string(),
            arch: "arm64".to_string(),
            extension_dir: "/usr/local/lib/php/extensions/no-debug-zts-20230831".to_string(),
            debug_suffix: String::new(),
            zts_suffix: "-zts".to_string(),
        };

        let error = validate_requested_metadata(&config, &metadata).unwrap_err();
        assert_eq!(
            error.to_string(),
            "non-ZTS build was requested, but selected PHP is ZTS; pass --zts"
        );
    }
}
