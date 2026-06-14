mod backend;
mod cli;
mod composer;
mod deb_package;
mod package;
mod rust_ext;
mod zip_package;

use anyhow::{Context, Result, anyhow, bail};
use backend::{BuildBackend, DockerLinux, NativeDarwin};
use cli::{ArtifactKind, BuildArgs, BuildKind, Libc, TargetOs};
pub use cli::{Cli, Commands};
use deb_package::DebPackageDetails;
use package::PackageDetails;
use std::path::{Path, PathBuf};

/// Runs the requested CLI command.
///
/// # Errors
///
/// Returns an error if command validation, building, metadata collection, or
/// package creation fails.
pub fn run(cli: Cli) -> Result<()> {
    match cli.command {
        Commands::Build(args) => {
            let package_paths = build(args)?;
            for package_path in package_paths {
                println!("{}", package_path.display());
            }
            Ok(())
        }
    }
}

/// Builds and packages a PHP extension.
///
/// # Errors
///
/// Returns an error if the build arguments are invalid, required project
/// metadata cannot be read, the selected backend fails, or the output artifacts
/// cannot be created.
pub fn build(args: BuildArgs) -> Result<Vec<PathBuf>> {
    let config = BuildConfig::try_from(args)?;
    let extension_name = composer::extension_name_from_file("composer.json")?;

    let metadata = match config.target_os {
        TargetOs::Linux => DockerLinux.build(&config)?,
        TargetOs::Darwin => NativeDarwin.build(&config)?,
    };

    let so_path = resolve_so_path(&config, &extension_name)?;
    let mut output_paths = Vec::new();

    for artifact in &config.artifacts {
        match artifact {
            ArtifactKind::Zip => {
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
                let output_path = config.out_dir.join(package.filename());

                zip_package::create_zip(&so_path, &output_path, &format!("{extension_name}.so"))
                    .with_context(|| format!("failed to package {}", so_path.display()))?;
                output_paths.push(output_path);
            }
            ArtifactKind::Deb => {
                let package = DebPackageDetails {
                    extension_name: &extension_name,
                    package_version: &config.package_version,
                    php_major_minor: &metadata.php_major_minor,
                    arch: &metadata.arch,
                    extension_dir: &metadata.extension_dir,
                };
                let output_path = config.out_dir.join(package.filename()?);

                deb_package::create_deb(&so_path, &output_path, &package)
                    .with_context(|| format!("failed to package {}", so_path.display()))?;
                output_paths.push(output_path);
            }
        }
    }

    Ok(output_paths)
}

fn resolve_so_path(config: &BuildConfig, extension_name: &str) -> Result<PathBuf> {
    let modules = config
        .build_path
        .join("modules")
        .join(format!("{extension_name}.so"));

    if config.extension_kind == BuildKind::C {
        return Ok(modules);
    }

    let mut candidates = vec![
        modules,
        config.build_path.join(format!("{extension_name}.so")),
    ];

    if let Some(crate_name) = rust_ext::crate_name(&config.build_path) {
        let libraries = [
            format!("lib{crate_name}.so"),
            format!("lib{crate_name}.dylib"),
        ];
        for library in &libraries {
            candidates.push(
                config
                    .build_path
                    .join("target")
                    .join("release")
                    .join(library),
            );
            if let Some(workspace_target) = workspace_target_dir(&config.build_path) {
                candidates.push(workspace_target.join("release").join(library));
            }
        }
    }

    candidates
        .into_iter()
        .find(|candidate| candidate.exists())
        .ok_or_else(|| {
            anyhow!(
                "could not locate the built .so for Rust extension '{extension_name}' under {}",
                config.build_path.display()
            )
        })
}

fn workspace_target_dir(build_path: &Path) -> Option<PathBuf> {
    for ancestor in build_path.ancestors().skip(1) {
        let target = ancestor.join("target");
        if target.is_dir() {
            return Some(target);
        }
    }

    None
}

#[derive(Debug, Clone)]
pub struct BuildConfig {
    pub package_version: String,
    pub artifacts: Vec<ArtifactKind>,
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
    pub extension_kind: BuildKind,
    pub cargo_features: Vec<String>,
}

impl TryFrom<BuildArgs> for BuildConfig {
    type Error = anyhow::Error;

    fn try_from(args: BuildArgs) -> Result<Self> {
        let artifacts = selected_artifacts(args.artifact);
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

        let extension_kind = match args.build_kind {
            Some(kind) => kind,
            None if rust_ext::is_rust_extension(&args.build_path)? => BuildKind::Rust,
            None => BuildKind::C,
        };

        if extension_kind == BuildKind::Rust {
            if !args.configure_flag.is_empty() {
                bail!(
                    "--configure-flag is not supported for Rust builds; use --cargo-feature instead"
                );
            }
        } else if !args.cargo_feature.is_empty() {
            bail!("--cargo-feature is only supported for Rust builds");
        }

        if artifacts.contains(&ArtifactKind::Deb) {
            if args.target_os != TargetOs::Linux {
                bail!("--artifact deb is only supported for linux builds");
            }

            if libc != Libc::Glibc {
                bail!("--artifact deb is only supported for glibc linux builds");
            }

            if args.zts {
                bail!("--artifact deb is only supported for non-ZTS linux builds");
            }
        }

        Ok(Self {
            package_version: args.package_version,
            artifacts,
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
            extension_kind,
            cargo_features: args.cargo_feature,
        })
    }
}

fn selected_artifacts(artifacts: Vec<ArtifactKind>) -> Vec<ArtifactKind> {
    if artifacts.is_empty() {
        return vec![ArtifactKind::Zip];
    }

    let mut selected = Vec::new();
    for artifact in artifacts {
        if !selected.contains(&artifact) {
            selected.push(artifact);
        }
    }

    selected
}

#[cfg(test)]
mod tests {
    use super::{BuildConfig, selected_artifacts};
    use crate::cli::{ArtifactKind, BuildArgs, BuildKind, Libc, TargetOs};
    use std::path::PathBuf;

    fn args(target_os: TargetOs) -> BuildArgs {
        BuildArgs {
            package_version: "1.2.3".to_string(),
            artifact: Vec::new(),
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
            build_kind: None,
            cargo_feature: Vec::new(),
        }
    }

    #[test]
    fn defaults_linux_to_glibc() {
        let config = BuildConfig::try_from(args(TargetOs::Linux)).unwrap();

        assert_eq!(config.libc, Libc::Glibc);
        assert_eq!(config.artifacts, vec![ArtifactKind::Zip]);
    }

    #[test]
    fn defaults_darwin_to_bsdlibc() {
        let mut build_args = args(TargetOs::Darwin);
        build_args.php_version = None;
        let config = BuildConfig::try_from(build_args).unwrap();

        assert_eq!(config.libc, Libc::Bsdlibc);
    }

    #[test]
    fn defaults_artifacts_to_zip() {
        assert_eq!(selected_artifacts(Vec::new()), vec![ArtifactKind::Zip]);
    }

    #[test]
    fn preserves_artifact_order_and_removes_duplicates() {
        assert_eq!(
            selected_artifacts(vec![
                ArtifactKind::Deb,
                ArtifactKind::Zip,
                ArtifactKind::Deb
            ]),
            vec![ArtifactKind::Deb, ArtifactKind::Zip]
        );
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

    #[test]
    fn rejects_deb_for_darwin() {
        let mut build_args = args(TargetOs::Darwin);
        build_args.artifact = vec![ArtifactKind::Deb];

        let error = BuildConfig::try_from(build_args).unwrap_err();
        assert_eq!(
            error.to_string(),
            "--artifact deb is only supported for linux builds"
        );
    }

    #[test]
    fn rejects_deb_for_musl() {
        let mut build_args = args(TargetOs::Linux);
        build_args.artifact = vec![ArtifactKind::Deb];
        build_args.libc = Some(Libc::Musl);

        let error = BuildConfig::try_from(build_args).unwrap_err();
        assert_eq!(
            error.to_string(),
            "--artifact deb is only supported for glibc linux builds"
        );
    }

    #[test]
    fn rejects_deb_for_zts() {
        let mut build_args = args(TargetOs::Linux);
        build_args.artifact = vec![ArtifactKind::Deb];
        build_args.zts = true;

        let error = BuildConfig::try_from(build_args).unwrap_err();
        assert_eq!(
            error.to_string(),
            "--artifact deb is only supported for non-ZTS linux builds"
        );
    }

    #[test]
    fn defaults_to_c_extension_kind() {
        let config = BuildConfig::try_from(args(TargetOs::Linux)).unwrap();

        assert_eq!(config.extension_kind, BuildKind::C);
    }

    #[test]
    fn honors_explicit_rust_build_kind() {
        let mut build_args = args(TargetOs::Linux);
        build_args.build_kind = Some(BuildKind::Rust);
        build_args.cargo_feature = vec!["closure".to_string()];

        let config = BuildConfig::try_from(build_args).unwrap();

        assert_eq!(config.extension_kind, BuildKind::Rust);
        assert_eq!(config.cargo_features, vec!["closure"]);
    }

    #[test]
    fn allows_rust_on_darwin() {
        let mut build_args = args(TargetOs::Darwin);
        build_args.php_version = None;
        build_args.build_kind = Some(BuildKind::Rust);

        let config = BuildConfig::try_from(build_args).unwrap();

        assert_eq!(config.extension_kind, BuildKind::Rust);
        assert_eq!(config.target_os, TargetOs::Darwin);
    }

    #[test]
    fn rejects_configure_flag_for_rust() {
        let mut build_args = args(TargetOs::Linux);
        build_args.build_kind = Some(BuildKind::Rust);
        build_args.configure_flag = vec!["--enable-foo".to_string()];

        let error = BuildConfig::try_from(build_args).unwrap_err();
        assert_eq!(
            error.to_string(),
            "--configure-flag is not supported for Rust builds; use --cargo-feature instead"
        );
    }

    #[test]
    fn rejects_cargo_feature_for_c() {
        let mut build_args = args(TargetOs::Linux);
        build_args.cargo_feature = vec!["closure".to_string()];

        let error = BuildConfig::try_from(build_args).unwrap_err();
        assert_eq!(
            error.to_string(),
            "--cargo-feature is only supported for Rust builds"
        );
    }
}
