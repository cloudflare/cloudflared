package diagnostic

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CreateDiagnosticZipFile create a zip file with the contents from the all
// files paths. The files will be written in the root of the zip file.
// In case of an error occurs after whilst writing to the zip file
// this will be removed.
func CreateDiagnosticZipFile(base string, paths []string) (zipFileName string, err error) {
	// Create a zip file with all files from paths added to the root
	suffix := time.Now().Format(time.RFC3339)
	zipFileName = base + "-" + suffix + ".zip"
	zipFileName = strings.ReplaceAll(zipFileName, ":", "-")

	archive, cerr := os.Create(zipFileName)
	if cerr != nil {
		return "", fmt.Errorf("error creating file %s: %w", zipFileName, cerr)
	}

	archiveWriter := zip.NewWriter(archive)

	defer func() {
		archiveWriter.Close()
		archive.Close()

		if err != nil {
			os.Remove(zipFileName)
		}
	}()

	for _, file := range paths {
		if file == "" {
			continue
		}

		var handle *os.File

		handle, err = os.Open(file)
		if err != nil {
			return "", fmt.Errorf("error opening file %s: %w", zipFileName, err)
		}

		defer handle.Close()

		// Keep the base only to not create sub directories in the
		// zip file.
		var writer io.Writer

		writer, err = archiveWriter.Create(filepath.Base(file))
		if err != nil {
			return "", fmt.Errorf("error creating archive writer from %s: %w", file, err)
		}

		if _, err = io.Copy(writer, handle); err != nil {
			return "", fmt.Errorf("error copying file %s: %w", file, err)
		}
	}

	zipFileName = archive.Name()
	return zipFileName, nil
}
