use anyhow::{Context, Result};
use regex::Regex;
use std::fs;
use std::path::Path;
use std::sync::LazyLock;

static EXT_PHP_RS_DEP_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?m)^\s*(ext-php-rs\s*(=|\.)|\[[^\]]*\bdependencies\.ext-php-rs\b)")
        .expect("valid ext-php-rs dependency regex")
});

static TABLE_HEADER_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^\s*\[([^\]]+)\]").expect("valid table header regex"));

static NAME_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r#"^\s*name\s*=\s*"([^"]+)""#).expect("valid name regex"));

pub fn is_rust_extension(build_path: &Path) -> Result<bool> {
    let cargo = build_path.join("Cargo.toml");
    if !cargo.exists() {
        return Ok(false);
    }

    let contents = fs::read_to_string(&cargo)
        .with_context(|| format!("failed to read {}", cargo.display()))?;
    Ok(declares_ext_php_rs(&contents))
}

pub fn crate_name(build_path: &Path) -> Option<String> {
    let contents = fs::read_to_string(build_path.join("Cargo.toml")).ok()?;
    cdylib_name(&contents).map(|name| name.replace('-', "_"))
}

fn declares_ext_php_rs(contents: &str) -> bool {
    EXT_PHP_RS_DEP_RE.is_match(contents)
}

fn cdylib_name(contents: &str) -> Option<String> {
    let mut current_table: Option<String> = None;
    let mut package_name: Option<String> = None;
    let mut lib_name: Option<String> = None;

    for line in contents.lines() {
        if let Some(captures) = TABLE_HEADER_RE.captures(line) {
            current_table = Some(captures[1].trim().to_string());
            continue;
        }

        let Some(table) = current_table.as_deref() else {
            continue;
        };
        if table != "package" && table != "lib" {
            continue;
        }

        if let Some(captures) = NAME_RE.captures(line) {
            let value = captures[1].to_string();
            match table {
                "lib" => lib_name = Some(value),
                _ => package_name = Some(value),
            }
        }
    }

    lib_name.or(package_name)
}

#[cfg(test)]
mod tests {
    use super::{cdylib_name, declares_ext_php_rs};

    #[test]
    fn detects_simple_dependency() {
        assert!(declares_ext_php_rs(
            "[dependencies]\next-php-rs = \"0.12\"\n"
        ));
    }

    #[test]
    fn detects_table_dependency() {
        assert!(declares_ext_php_rs(
            "[dependencies.ext-php-rs]\nversion = \"0.12\"\n"
        ));
    }

    #[test]
    fn detects_workspace_dependency() {
        assert!(declares_ext_php_rs(
            "[dependencies]\next-php-rs.workspace = true\n"
        ));
    }

    #[test]
    fn ignores_commented_dependency() {
        assert!(!declares_ext_php_rs(
            "[dependencies]\n# ext-php-rs = \"0.12\"\n"
        ));
    }

    #[test]
    fn ignores_unrelated_manifest() {
        assert!(!declares_ext_php_rs("[dependencies]\nserde = \"1\"\n"));
    }

    #[test]
    fn prefers_lib_name_over_package_name() {
        let manifest = "[package]\nname = \"my-ext\"\n\n[lib]\nname = \"different_name\"\ncrate-type = [\"cdylib\"]\n";

        assert_eq!(cdylib_name(manifest).as_deref(), Some("different_name"));
    }

    #[test]
    fn falls_back_to_package_name() {
        let manifest = "[package]\nname = \"my-ext\"\n";

        assert_eq!(cdylib_name(manifest).as_deref(), Some("my-ext"));
    }

    #[test]
    fn ignores_name_in_other_tables() {
        let manifest = "[[bin]]\nname = \"helper\"\n\n[package]\nname = \"real_pkg\"\n";

        assert_eq!(cdylib_name(manifest).as_deref(), Some("real_pkg"));
    }

    #[test]
    fn returns_none_without_package_or_lib_name() {
        assert_eq!(cdylib_name("[workspace]\nmembers = [\"a\"]\n"), None);
    }
}
