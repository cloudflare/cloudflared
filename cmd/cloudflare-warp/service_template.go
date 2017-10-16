package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"text/template"

	homedir "github.com/mitchellh/go-homedir"
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
		return fmt.Errorf("error getting stderr pipe: %v", err)
	}
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("error starting %s: %v", command, err)
	}
	commandErr, _ := ioutil.ReadAll(stderr)
	if len(commandErr) > 0 {
		return fmt.Errorf("%s error: %s", command, commandErr)
	}
	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("%s returned with error: %v", command, err)
	}
	return nil
}
