use clap::{Parser, Subcommand, ValueEnum};
use std::path::PathBuf;

#[derive(Debug, Parser)]
#[command(name = "php-extension-builder")]
#[command(about = "Build PHP extension pre-packaged binary ZIPs")]
pub struct Cli {
    #[command(subcommand)]
    pub command: Commands,
}

#[derive(Debug, Subcommand)]
pub enum Commands {
    Build(BuildArgs),
}

#[derive(Debug, Clone, Parser)]
pub struct BuildArgs {
    #[arg(long)]
    pub package_version: String,

    #[arg(long)]
    pub php_version: Option<String>,

    #[arg(long, value_enum, default_value_t = TargetOs::Linux)]
    pub target_os: TargetOs,

    #[arg(long, value_enum)]
    pub libc: Option<Libc>,

    #[arg(long)]
    pub zts: bool,

    #[arg(long, default_value = ".")]
    pub build_path: PathBuf,

    #[arg(long = "configure-flag", allow_hyphen_values = true)]
    pub configure_flag: Vec<String>,

    #[arg(long = "apt-package", allow_hyphen_values = true)]
    pub apt_package: Vec<String>,

    #[arg(long = "apk-package", allow_hyphen_values = true)]
    pub apk_package: Vec<String>,

    #[arg(long, default_value = ".")]
    pub out_dir: PathBuf,

    #[arg(long)]
    pub image: Option<String>,

    #[arg(long)]
    pub php_config: Option<PathBuf>,
}

#[derive(Debug, Copy, Clone, Eq, PartialEq, ValueEnum)]
pub enum TargetOs {
    Linux,
    Darwin,
}

impl TargetOs {
    pub fn as_package_str(self) -> &'static str {
        match self {
            Self::Linux => "linux",
            Self::Darwin => "darwin",
        }
    }
}

#[derive(Debug, Copy, Clone, Eq, PartialEq, ValueEnum)]
pub enum Libc {
    Glibc,
    Musl,
    Bsdlibc,
}

impl Libc {
    pub fn as_package_str(self) -> &'static str {
        match self {
            Self::Glibc => "glibc",
            Self::Musl => "musl",
            Self::Bsdlibc => "bsdlibc",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{Cli, Commands};
    use clap::Parser;

    #[test]
    fn accepts_repeated_hyphenated_configure_flags() {
        let cli = Cli::try_parse_from([
            "php-extension-builder",
            "build",
            "--package-version",
            "1.2.3",
            "--php-version",
            "8.3",
            "--configure-flag",
            "--enable-example-pie-extension",
            "--configure-flag",
            "--with-hello-name=FROM_CLI",
        ])
        .unwrap();

        let Commands::Build(args) = cli.command;
        assert_eq!(
            args.configure_flag,
            vec![
                "--enable-example-pie-extension",
                "--with-hello-name=FROM_CLI"
            ]
        );
    }

    #[test]
    fn accepts_custom_apt_and_apk_packages() {
        let cli = Cli::try_parse_from([
            "php-extension-builder",
            "build",
            "--package-version",
            "1.2.3",
            "--php-version",
            "8.3",
            "--apt-package",
            "libzstd-dev",
            "--apk-package",
            "zstd-dev",
        ])
        .unwrap();

        let Commands::Build(args) = cli.command;
        assert_eq!(args.apt_package, vec!["libzstd-dev"]);
        assert_eq!(args.apk_package, vec!["zstd-dev"]);
    }
}
