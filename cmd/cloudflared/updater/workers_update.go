package updater

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"
)

const (
	clientTimeout = time.Second * 60
	// stop the service
	// rename cloudflared.exe to cloudflared.exe.old
	// rename cloudflared.exe.new to cloudflared.exe
	// delete cloudflared.exe.old
	// start the service
	// exit with code 0 if we've reached this point indicating success.
	windowsUpdateCommandTemplate = `sc stop cloudflared >nul 2>&1
rename "{{.TargetPath}}" {{.OldName}}
rename "{{.NewPath}}" {{.BinaryName}}
del "{{.OldPath}}"
sc start cloudflared >nul 2>&1
exit /b 0`
	batchFileName = "cfd_update.bat"
)

// Prepare some data to insert into the template.
type batchData struct {
	TargetPath string
	OldName    string
	NewPath    string
	OldPath    string
	BinaryName string
	BatchName  string
}

// WorkersVersion implements the Version interface.
// It contains everything needed to perform a version upgrade
type WorkersVersion struct {
	downloadURL  string
	checksum     string
	version      string
	targetPath   string
	isCompressed bool
	userMessage  string
}

// NewWorkersVersion creates a new Version object. This is normally created by a WorkersService JSON checkin response
// url is where to download the file
// version is the version of this update
// checksum is the expected checksum of the downloaded file
// target path is where the file should be replace. Normally this the running cloudflared's path
// userMessage is a possible message to convey back to the user after having checked in with the Updater Service
// isCompressed tells whether the asset to update cloudflared is compressed or not
func NewWorkersVersion(url, version, checksum, targetPath, userMessage string, isCompressed bool) CheckResult {
	return &WorkersVersion{
		downloadURL:  url,
		version:      version,
		checksum:     checksum,
		targetPath:   targetPath,
		isCompressed: isCompressed,
		userMessage:  userMessage,
	}
}

// Apply does the actual verification and update logic.
// This includes signature and checksum validation,
// replacing the binary, etc
func (v *WorkersVersion) Apply() error {
	newFilePath := fmt.Sprintf("%s.new", v.targetPath)
	os.Remove(newFilePath) //remove any failed updates before download

	// download the file
	if err := download(v.downloadURL, newFilePath, v.isCompressed); err != nil {
		return err
	}

	// check that the file is what is expected
	if err := isValidChecksum(v.checksum, newFilePath); err != nil {
		return err
	}

	oldFilePath := fmt.Sprintf("%s.old", v.targetPath)
	// Windows requires more effort to self update, especially when it is running as a service:
	// you have to stop the service (if running as one) in order to move/rename the binary
	// but now the binary isn't running though, so an external process
	// has to move the old binary out and the new one in then start the service
	// the easiest way to do this is with a batch file (or with a DLL, but that gets ugly for a cross compiled binary like cloudflared)
	// a batch file isn't ideal, but it is the simplest path forward for the constraints Windows creates
	if runtime.GOOS == "windows" {
		if err := writeBatchFile(v.targetPath, newFilePath, oldFilePath); err != nil {
			return err
		}
		rootDir := filepath.Dir(v.targetPath)
		batchPath := filepath.Join(rootDir, batchFileName)
		return runWindowsBatch(batchPath)
	}

	// now move the current file out, move the new file in and delete the old file
	if err := os.Rename(v.targetPath, oldFilePath); err != nil {
		return err
	}

	if err := os.Rename(newFilePath, v.targetPath); err != nil {
		//attempt rollback
		os.Rename(oldFilePath, v.targetPath)
		return err
	}
	os.Remove(oldFilePath)

	return nil
}

// String returns the version number of this update/release (e.g. 2020.08.05)
func (v *WorkersVersion) Version() string {
	return v.version
}

// String returns a possible message to convey back to user after having checked in with the Updater Service. E.g.
// it can warn about the need to update the version currently running.
func (v *WorkersVersion) UserMessage() string {
	return v.userMessage
}

// download the file from the link in the json
func download(url, filepath string, isCompressed bool) error {
	client := &http.Client{
		Timeout: clientTimeout,
	}
	resp, err := client.Get(url)

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var r io.Reader
	r = resp.Body

	// compressed macos binary, need to decompress
	if isCompressed || isCompressedFile(url) {
		// first the gzip reader
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		defer gr.Close()

		// now the tar
		tr := tar.NewReader(gr)

		// advance the reader pass the header, which will be the single binary file
		tr.Next()

		r = tr
	}

	out, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, r)
	return err
}

// isCompressedFile is a really simple file extension check to see if this is a macos tar and gzipped
func isCompressedFile(urlstring string) bool {
	if strings.HasSuffix(urlstring, ".tgz") {
		return true
	}

	u, err := url.Parse(urlstring)
	if err != nil {
		return false
	}
	return strings.HasSuffix(u.Path, ".tgz")
}

// checks if the checksum in the json response matches the checksum of the file download
func isValidChecksum(checksum, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	hash := fmt.Sprintf("%x", h.Sum(nil))

	if checksum != hash {
		return errors.New("checksum validation failed")
	}
	return nil
}

// writeBatchFile writes a batch file out to disk
// see the dicussion on why it has to be done this way
func writeBatchFile(targetPath string, newPath string, oldPath string) error {
	batchFilePath := filepath.Join(filepath.Dir(targetPath), batchFileName)
	os.Remove(batchFilePath) //remove any failed updates before download
	f, err := os.Create(batchFilePath)
	if err != nil {
		return err
	}
	defer f.Close()
	cfdName := filepath.Base(targetPath)
	oldName := filepath.Base(oldPath)

	data := batchData{
		TargetPath: targetPath,
		OldName:    oldName,
		NewPath:    newPath,
		OldPath:    oldPath,
		BinaryName: cfdName,
		BatchName:  batchFileName,
	}

	t, err := template.New("batch").Parse(windowsUpdateCommandTemplate)
	if err != nil {
		return err
	}
	return t.Execute(f, data)
}

// run each OS command for windows
func runWindowsBatch(batchFile string) error {
	defer os.Remove(batchFile)
	cmd := exec.Command("cmd", "/C", batchFile)
	_, err := cmd.Output()
	// Remove the batch file we created. Don't let this interfere with the error
	// we report.
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("Error during update : %s;", string(exitError.Stderr))
		}

	}
	return err
}
