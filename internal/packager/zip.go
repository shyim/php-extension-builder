package packager

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func CreateZip(sourceFile, outputFile, entryName string) error {
	parent := filepath.Dir(outputFile)
	if parent != "" && parent != "." {
		if err := os.MkdirAll(parent, 0755); err != nil {
			return fmt.Errorf("failed to create output directory %s: %w", parent, err)
		}
	}

	source, err := os.Open(sourceFile)
	if err != nil {
		return fmt.Errorf("failed to open built extension %s: %w", sourceFile, err)
	}
	defer source.Close()

	output, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create package %s: %w", outputFile, err)
	}
	defer output.Close()

	zipWriter := zip.NewWriter(output)
	defer zipWriter.Close()

	header := &zip.FileHeader{
		Name:   entryName,
		Method: zip.Deflate,
	}
	header.SetMode(0644)

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("failed to add %s to package: %w", entryName, err)
	}

	if _, err := io.Copy(writer, source); err != nil {
		return fmt.Errorf("failed to write %s to package: %w", entryName, err)
	}

	if err := zipWriter.Close(); err != nil {
		return fmt.Errorf("failed to finish zip package: %w", err)
	}

	return nil
}
