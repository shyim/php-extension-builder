use crate::BuildConfig;
use crate::cli::Libc;
use crate::package::php_major_minor;
use anyhow::{Context, Result, anyhow, bail};
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::process::{Command, Output};

const META_PREFIX: &str = "__PIE_META_";

#[derive(Debug, Clone, Eq, PartialEq)]
pub struct BuildMetadata {
    pub php_major_minor: String,
    pub arch: String,
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

        let output = command.output().context("failed to start docker")?;
        write_output(&output)?;
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
        let metadata = native_metadata(config, php_config)?;
        validate_requested_metadata(config, &metadata)?;

        run_native(
            CommandSpec::new("phpize", &[], &config.build_path),
            "phpize",
        )?;

        let mut configure_flags = config.configure_flags.clone();
        if let Some(php_config_path) = &config.php_config
            && !configure_flags
                .iter()
                .any(|flag| flag.starts_with("--with-php-config"))
        {
            configure_flags.push(format!("--with-php-config={}", php_config_path.display()));
        }
        run_native(
            CommandSpec::new("./configure", &configure_flags, &config.build_path),
            "./configure",
        )?;
        run_native(CommandSpec::new("make", &[], &config.build_path), "make")?;

        Ok(metadata)
    }
}

fn native_metadata(config: &BuildConfig, php_config: &Path) -> Result<BuildMetadata> {
    let php_version = command_stdout(php_config, &["--version"], &config.build_path)
        .context("failed to run php-config --version")?;
    let php_binary = command_stdout(php_config, &["--php-binary"], &config.build_path)
        .context("failed to run php-config --php-binary")?;
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
    let output = Command::new(spec.program)
        .args(spec.args)
        .current_dir(spec.cwd)
        .output()
        .with_context(|| format!("failed to start {label}"))?;

    write_output(&output)?;
    ensure_success(&output, label)
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

fn write_output(output: &Output) -> Result<()> {
    io::stdout()
        .write_all(&output.stdout)
        .context("failed to write command stdout")?;
    io::stderr()
        .write_all(&output.stderr)
        .context("failed to write command stderr")?;
    Ok(())
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
        (Libc::Glibc, false) => "cli".to_string(),
        (Libc::Glibc, true) => "zts".to_string(),
        (Libc::Musl, false) => "cli-alpine".to_string(),
        (Libc::Musl, true) => "zts-alpine".to_string(),
        (Libc::Bsdlibc, _) => bail!("bsdlibc is not a docker linux target"),
    };

    Ok(format!("php:{php_version}-{suffix}"))
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
if command -v apk >/dev/null 2>&1; then
  apk add --no-cache ${{PHPIZE_DEPS:-autoconf dpkg-dev dpkg file g++ gcc libc-dev make pkgconf re2c}} {alpine_package_args}
elif command -v apt-get >/dev/null 2>&1; then
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ${{PHPIZE_DEPS:-autoconf dpkg-dev file g++ gcc libc-dev make pkg-config re2c}} {debian_package_args}
  rm -rf /var/lib/apt/lists/*
fi
phpize
{configure}
make
php_binary="$(php-config --php-binary)"
if [ "$php_binary" = "NONE" ]; then
  php_binary=php
fi
printf '{META_PREFIX}PHP_VERSION=%s\n' "$(php-config --version)"
printf '{META_PREFIX}ARCH=%s\n' "$(uname -m)"
printf '{META_PREFIX}DEBUG=%s\n' "$("$php_binary" -n -r "echo PHP_DEBUG ? '-debug' : '';")"
printf '{META_PREFIX}ZTS=%s\n' "$("$php_binary" -n -r "echo ZEND_THREAD_SAFE ? '-zts' : '';")"
if [ -n "${{HOST_UID:-}}" ] && [ -n "${{HOST_GID:-}}" ]; then
  chown -R "$HOST_UID:$HOST_GID" .
fi
"#
    )
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
    let mut debug_suffix = None;
    let mut zts_suffix = None;

    for line in stdout.lines() {
        if let Some(value) = line.strip_prefix(&format!("{META_PREFIX}PHP_VERSION=")) {
            php_version = Some(value.to_string());
        } else if let Some(value) = line.strip_prefix(&format!("{META_PREFIX}ARCH=")) {
            arch = Some(value.to_string());
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
    use crate::cli::{Libc, TargetOs};
    use std::path::Path;
    use std::path::PathBuf;

    fn linux_config(libc: Libc, zts: bool) -> BuildConfig {
        BuildConfig {
            package_version: "1.2.3".to_string(),
            php_version: Some("8.3".to_string()),
            target_os: TargetOs::Linux,
            libc,
            zts,
            build_path: PathBuf::from("."),
            configure_flags: Vec::new(),
            apt_packages: Vec::new(),
            apk_packages: Vec::new(),
            out_dir: PathBuf::from("."),
            image: None,
            php_config: None,
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
