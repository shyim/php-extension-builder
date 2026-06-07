use anyhow::{Context, Result};
use std::fs::{self, File};
use std::io::{Read, Write};
use std::path::Path;
use zip::write::FileOptions;

pub fn create_zip(source_file: &Path, output_file: &Path, entry_name: &str) -> Result<()> {
    let parent = output_file.parent().unwrap_or_else(|| Path::new("."));
    if !parent.as_os_str().is_empty() {
        fs::create_dir_all(parent)
            .with_context(|| format!("failed to create output directory {}", parent.display()))?;
    }

    let mut source = File::open(source_file)
        .with_context(|| format!("failed to open built extension {}", source_file.display()))?;
    let output = File::create(output_file)
        .with_context(|| format!("failed to create package {}", output_file.display()))?;

    let mut zip = zip::ZipWriter::new(output);
    let options = FileOptions::default()
        .compression_method(zip::CompressionMethod::Deflated)
        .unix_permissions(0o644);

    zip.start_file(entry_name, options)
        .with_context(|| format!("failed to add {entry_name} to package"))?;

    let mut buffer = Vec::new();
    source
        .read_to_end(&mut buffer)
        .with_context(|| format!("failed to read {}", source_file.display()))?;
    zip.write_all(&buffer)
        .with_context(|| format!("failed to write {entry_name} to package"))?;
    zip.finish().context("failed to finish zip package")?;

    Ok(())
}
