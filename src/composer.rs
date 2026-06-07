use anyhow::{Context, Result, bail};
use regex::Regex;
use serde::Deserialize;
use std::fs;
use std::path::Path;
use std::sync::LazyLock;

static EXTENSION_NAME_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[A-Za-z][a-zA-Z0-9_]+$").expect("valid extension regex"));

#[derive(Debug, Deserialize)]
struct ComposerJson {
    #[serde(rename = "type")]
    package_type: Option<String>,
    name: Option<String>,
    #[serde(rename = "php-ext")]
    php_ext: Option<PhpExt>,
}

#[derive(Debug, Deserialize)]
struct PhpExt {
    #[serde(rename = "extension-name")]
    extension_name: Option<String>,
}

pub fn extension_name_from_file(path: impl AsRef<Path>) -> Result<String> {
    let path = path.as_ref();
    let contents = fs::read_to_string(path).with_context(|| {
        format!(
            "{} not found. This does not appear to be a PIE package.",
            path.display()
        )
    })?;

    extension_name_from_json(&contents)
}

fn extension_name_from_json(contents: &str) -> Result<String> {
    let composer: ComposerJson =
        serde_json::from_str(contents).context("failed to parse composer.json")?;

    let package_type = composer.package_type.as_deref().unwrap_or("null");
    if package_type != "php-ext" && package_type != "php-ext-zend" {
        bail!(
            "composer.json type must be \"php-ext\" or \"php-ext-zend\", but \"{package_type}\" was found."
        );
    }

    let mut extension_name = composer
        .php_ext
        .and_then(|php_ext| php_ext.extension_name)
        .unwrap_or_default();

    if extension_name.is_empty() {
        let package_name = composer.name.unwrap_or_default();
        if package_name.is_empty() {
            bail!(
                "Could not determine extension name: both .\"php-ext\".\"extension-name\" and .name are missing in composer.json"
            );
        }

        extension_name = package_name
            .rsplit('/')
            .next()
            .unwrap_or(package_name.as_str())
            .to_string();
    }

    if let Some(stripped) = extension_name.strip_prefix("ext-") {
        extension_name = stripped.to_string();
    }

    if !EXTENSION_NAME_RE.is_match(&extension_name) {
        bail!(
            "Invalid extension name: \"{extension_name}\" - must be alphanumeric/underscores only."
        );
    }

    Ok(extension_name)
}

#[cfg(test)]
mod tests {
    use super::extension_name_from_json;

    #[test]
    fn rejects_missing_or_invalid_type() {
        let error = extension_name_from_json(r#"{"type":"library"}"#).unwrap_err();
        assert_eq!(
            error.to_string(),
            "composer.json type must be \"php-ext\" or \"php-ext-zend\", but \"library\" was found."
        );
    }

    #[test]
    fn prefers_php_ext_extension_name_and_strips_ext_prefix() {
        let name = extension_name_from_json(
            r#"{"type":"php-ext","php-ext":{"extension-name":"ext-test_ext"},"name":"vendor/ignored"}"#,
        )
        .unwrap();

        assert_eq!(name, "test_ext");
    }

    #[test]
    fn falls_back_to_package_name_suffix() {
        let name =
            extension_name_from_json(r#"{"type":"php-ext-zend","name":"vendor/foo"}"#).unwrap();

        assert_eq!(name, "foo");
    }

    #[test]
    fn rejects_missing_extension_and_package_names() {
        let error = extension_name_from_json(r#"{"type":"php-ext"}"#).unwrap_err();
        assert_eq!(
            error.to_string(),
            "Could not determine extension name: both .\"php-ext\".\"extension-name\" and .name are missing in composer.json"
        );
    }

    #[test]
    fn rejects_invalid_extension_name() {
        let error = extension_name_from_json(
            r#"{"type":"php-ext","php-ext":{"extension-name":"invalid-ext-name"}}"#,
        )
        .unwrap_err();

        assert_eq!(
            error.to_string(),
            "Invalid extension name: \"invalid-ext-name\" - must be alphanumeric/underscores only."
        );
    }
}
