package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/mitchellh/go-homedir"
)

type ServiceTemplate struct {
	Path     string
	Content  string
	FileMode os.FileMode
}

type ServiceTemplateArgs struct {
	Path string
}

func (st *ServiceTemplate) ResolvePath() (string, error) {
	resolvedPath, err := homedir.Expand(st.Path)
	if err != nil {
		return "", fmt.Errorf("error resolving path %s: %v", st.Path, err)
	}
	return resolvedPath, nil
}

func (st *ServiceTemplate) Generate(args *ServiceTemplateArgs) error {
	tmpl, err := template.New(st.Path).Parse(st.Content)
	if err != nil {
		return fmt.Errorf("error generating %s template: %v", st.Path, err)
	}
	resolvedPath, err := st.ResolvePath()
	if err != nil {
		return err
	}
	var buffer bytes.Buffer
	err = tmpl.Execute(&buffer, args)
	if err != nil {
		return fmt.Errorf("error generating %s: %v", st.Path, err)
	}
	fileMode := os.FileMode(0644)
	if st.FileMode != 0 {
		fileMode = st.FileMode
	}
	err = ioutil.WriteFile(resolvedPath, buffer.Bytes(), fileMode)
	if err != nil {
		return fmt.Errorf("error writing %s: %v", resolvedPath, err)
	}
	return nil
}

func (st *ServiceTemplate) Remove() error {
	resolvedPath, err := st.ResolvePath()
	if err != nil {
		return err
	}
	err = os.Remove(resolvedPath)
	if err != nil {
		return fmt.Errorf("error deleting %s: %v", resolvedPath, err)
	}
	return nil
}

func runCommand(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		logger.WithError(err).Infof("error getting stderr pipe")
		return fmt.Errorf("error getting stderr pipe: %v", err)
	}
	err = cmd.Start()
	if err != nil {
		logger.WithError(err).Infof("error starting %s", command)
		return fmt.Errorf("error starting %s: %v", command, err)
	}
	commandErr, _ := ioutil.ReadAll(stderr)
	if len(commandErr) > 0 {
		logger.Errorf("%s: %s", command, commandErr)
	}
	err = cmd.Wait()
	if err != nil {
		logger.WithError(err).Infof("%s returned error", command)
		return fmt.Errorf("%s returned with error: %v", command, err)
	}
	return nil
}

func ensureConfigDirExists(configDir string) error {
	ok, err := fileExists(configDir)
	if !ok && err == nil {
		err = os.Mkdir(configDir, 0700)
	}
	return err
}

// openFile opens the file at path. If create is set and the file exists, returns nil, true, nil
func openFile(path string, create bool) (file *os.File, exists bool, err error) {
	expandedPath, err := homedir.Expand(path)
	if err != nil {
		return nil, false, err
	}
	if create {
		fileInfo, err := os.Stat(expandedPath)
		if err == nil && fileInfo.Size() > 0 {
			return nil, true, nil
		}
		file, err = os.OpenFile(expandedPath, os.O_RDWR|os.O_CREATE, 0600)
	} else {
		file, err = os.Open(expandedPath)
	}
	return file, false, err
}

func copyCertificate(srcConfigDir, destConfigDir, credentialFile string) error {
	destCredentialPath := filepath.Join(destConfigDir, credentialFile)
	destFile, exists, err := openFile(destCredentialPath, true)
	if err != nil {
		return err
	} else if exists {
		// credentials already exist, do nothing
		return nil
	}
	defer destFile.Close()

	srcCredentialPath := filepath.Join(srcConfigDir, credentialFile)
	srcFile, _, err := openFile(srcCredentialPath, false)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Copy certificate
	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		return fmt.Errorf("unable to copy %s to %s: %v", srcCredentialPath, destCredentialPath, err)
	}

	return nil
}

func copyCredentials(serviceConfigDir, defaultConfigDir, defaultConfigFile, defaultCredentialFile string) error {
	if err := ensureConfigDirExists(serviceConfigDir); err != nil {
		return err
	}

	if err := copyCertificate(defaultConfigDir, serviceConfigDir, defaultCredentialFile); err != nil {
		return err
	}

	// Copy or create config
	destConfigPath := filepath.Join(serviceConfigDir, defaultConfigFile)
	destFile, exists, err := openFile(destConfigPath, true)
	if err != nil {
		logger.WithError(err).Infof("cannot open %s", destConfigPath)
		return err
	} else if exists {
		// config already exists, do nothing
		return nil
	}
	defer destFile.Close()

	srcConfigPath := filepath.Join(defaultConfigDir, defaultConfigFile)
	srcFile, _, err := openFile(srcConfigPath, false)
	if err != nil {
		fmt.Println("Your service needs a config file that at least specifies the hostname option.")
		fmt.Println("Type in a hostname now, or leave it blank and create the config file later.")
		fmt.Print("Hostname: ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		if input == "" {
			return err
		}
		fmt.Fprintf(destFile, "hostname: %s\n", input)
	} else {
		defer srcFile.Close()
		_, err = io.Copy(destFile, srcFile)
		if err != nil {
			return fmt.Errorf("unable to copy %s to %s: %v", srcConfigPath, destConfigPath, err)
		}
		logger.Infof("Copied %s to %s", srcConfigPath, destConfigPath)
	}

	return nil
}
