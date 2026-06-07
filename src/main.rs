use clap::Parser;
use php_extension_builder::Cli;

fn main() {
    if let Err(error) = php_extension_builder::run(Cli::parse()) {
        eprintln!("error: {error:#}");
        std::process::exit(1);
    }
}
