mod backend;
mod cli;
mod composer;
mod package;
mod zip_package;

use anyhow::{Context, Result, bail};
use backend::{BuildBackend, DockerLinux, NativeDarwin};
use cli::{BuildArgs, Libc, TargetOs};
pub use cli::{Cli, Commands};
use package::PackageDetails;
use std::path::PathBuf;

/// Runs the requested CLI command.
///
/// # Errors
///
/// Returns an error if command validation, building, metadata collection, or
/// package creation fails.
pub fn run(cli: Cli) -> Result<()> {
    match cli.command {
        Commands::Build(args) => {
            let package_path = build(args)?;
            println!("{}", package_path.display());
            Ok(())
        }
    }
}

/// Builds and packages a PHP extension.
///
/// # Errors
///
/// Returns an error if the build arguments are invalid, required project
/// metadata cannot be read, the selected backend fails, or the output archive
/// cannot be created.
pub fn build(args: BuildArgs) -> Result<PathBuf> {
    let config = BuildConfig::try_from(args)?;
    let extension_name = composer::extension_name_from_file("composer.json")?;

    let metadata = match config.target_os {
        TargetOs::Linux => DockerLinux.build(&config)?,
        TargetOs::Darwin => NativeDarwin.build(&config)?,
    };

    let package = PackageDetails {
        extension_name: &extension_name,
        package_version: &config.package_version,
        php_major_minor: &metadata.php_major_minor,
        arch: &metadata.arch,
        os: config.target_os.as_package_str(),
        libc: config.libc.as_package_str(),
        debug_suffix: &metadata.debug_suffix,
        zts_suffix: &metadata.zts_suffix,
    };
    let package_name = package.filename();
    let output_path = config.out_dir.join(package_name);
    let so_path = config
        .build_path
        .join("modules")
        .join(format!("{extension_name}.so"));

    zip_package::create_zip(&so_path, &output_path, &format!("{extension_name}.so"))
        .with_context(|| format!("failed to package {}", so_path.display()))?;

    Ok(output_path)
}

#[derive(Debug, Clone)]
pub struct BuildConfig {
    pub package_version: String,
    pub php_version: Option<String>,
    pub target_os: TargetOs,
    pub libc: Libc,
    pub zts: bool,
    pub build_path: PathBuf,
    pub configure_flags: Vec<String>,
    pub before_phpize_commands: Vec<String>,
    pub apt_packages: Vec<String>,
    pub apk_packages: Vec<String>,
    pub out_dir: PathBuf,
    pub image: Option<String>,
    pub php_config: Option<PathBuf>,
}

impl TryFrom<BuildArgs> for BuildConfig {
    type Error = anyhow::Error;

    fn try_from(args: BuildArgs) -> Result<Self> {
        let libc = args.libc.unwrap_or(match args.target_os {
            TargetOs::Linux => Libc::Glibc,
            TargetOs::Darwin => Libc::Bsdlibc,
        });

        match (args.target_os, libc) {
            (TargetOs::Linux, Libc::Bsdlibc) => {
                bail!("linux builds support only glibc or musl libc targets")
            }
            (TargetOs::Darwin, Libc::Glibc | Libc::Musl) => {
                bail!("darwin builds support only bsdlibc")
            }
            _ => {}
        }

        if args.target_os == TargetOs::Linux && args.php_version.is_none() {
            bail!("--php-version is required for linux Docker builds");
        }

        if args.target_os == TargetOs::Darwin && args.image.is_some() {
            bail!("--image is only supported for linux Docker builds");
        }

        if args.target_os == TargetOs::Darwin
            && (!args.apt_package.is_empty() || !args.apk_package.is_empty())
        {
            bail!("--apt-package and --apk-package are only supported for linux Docker builds");
        }

        Ok(Self {
            package_version: args.package_version,
            php_version: args.php_version,
            target_os: args.target_os,
            libc,
            zts: args.zts,
            build_path: args.build_path,
            configure_flags: args.configure_flag,
            before_phpize_commands: args.before_phpize_command,
            apt_packages: args.apt_package,
            apk_packages: args.apk_package,
            out_dir: args.out_dir,
            image: args.image,
            php_config: args.php_config,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::BuildConfig;
    use crate::cli::{BuildArgs, Libc, TargetOs};
    use std::path::PathBuf;

    fn args(target_os: TargetOs) -> BuildArgs {
        BuildArgs {
            package_version: "1.2.3".to_string(),
            php_version: Some("8.3".to_string()),
            target_os,
            libc: None,
            zts: false,
            build_path: PathBuf::from("."),
            configure_flag: Vec::new(),
            before_phpize_command: Vec::new(),
            apt_package: Vec::new(),
            apk_package: Vec::new(),
            out_dir: PathBuf::from("."),
            image: None,
            php_config: None,
        }
    }

    #[test]
    fn defaults_linux_to_glibc() {
        let config = BuildConfig::try_from(args(TargetOs::Linux)).unwrap();

        assert_eq!(config.libc, Libc::Glibc);
    }

    #[test]
    fn defaults_darwin_to_bsdlibc() {
        let mut build_args = args(TargetOs::Darwin);
        build_args.php_version = None;
        let config = BuildConfig::try_from(build_args).unwrap();

        assert_eq!(config.libc, Libc::Bsdlibc);
    }

    #[test]
    fn requires_php_version_for_linux() {
        let mut build_args = args(TargetOs::Linux);
        build_args.php_version = None;

        let error = BuildConfig::try_from(build_args).unwrap_err();
        assert_eq!(
            error.to_string(),
            "--php-version is required for linux Docker builds"
        );
    }

    #[test]
    fn rejects_darwin_docker_image_override() {
        let mut build_args = args(TargetOs::Darwin);
        build_args.image = Some("php:8.3-cli".to_string());

        let error = BuildConfig::try_from(build_args).unwrap_err();
        assert_eq!(
            error.to_string(),
            "--image is only supported for linux Docker builds"
        );
    }

    #[test]
    fn rejects_darwin_container_packages() {
        let mut build_args = args(TargetOs::Darwin);
        build_args.apt_package = vec!["libzstd-dev".to_string()];

        let error = BuildConfig::try_from(build_args).unwrap_err();
        assert_eq!(
            error.to_string(),
            "--apt-package and --apk-package are only supported for linux Docker builds"
        );
    }
}
