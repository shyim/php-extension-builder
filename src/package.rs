pub struct PackageDetails<'a> {
    pub extension_name: &'a str,
    pub package_version: &'a str,
    pub php_major_minor: &'a str,
    pub arch: &'a str,
    pub os: &'a str,
    pub libc: &'a str,
    pub debug_suffix: &'a str,
    pub zts_suffix: &'a str,
}

impl PackageDetails<'_> {
    pub fn filename(&self) -> String {
        format!(
            "php_{}-{}_php{}-{}-{}-{}{}{}.zip",
            self.extension_name,
            self.package_version,
            self.php_major_minor,
            self.arch,
            self.os,
            self.libc,
            self.debug_suffix,
            self.zts_suffix
        )
    }
}

pub fn php_major_minor(version: &str) -> String {
    version
        .trim()
        .split('.')
        .take(2)
        .collect::<Vec<_>>()
        .join(".")
}

#[cfg(test)]
mod tests {
    use super::{PackageDetails, php_major_minor};

    #[test]
    fn creates_pie_package_filename() {
        let details = PackageDetails {
            extension_name: "foo",
            package_version: "1.2.3",
            php_major_minor: "8.3",
            arch: "x86_64",
            os: "linux",
            libc: "glibc",
            debug_suffix: "-debug",
            zts_suffix: "-zts",
        };

        assert_eq!(
            details.filename(),
            "php_foo-1.2.3_php8.3-x86_64-linux-glibc-debug-zts.zip"
        );
    }

    #[test]
    fn trims_php_version_to_major_minor() {
        assert_eq!(php_major_minor("8.3.10-whatever\n"), "8.3");
    }
}
