package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	homedir "github.com/mitchellh/go-homedir"
)

type ServiceTemplate struct {
	Path     string
	Content  string
	FileMode os.FileMode
}

type ServiceTemplateArgs struct {
	Path      string
	ExtraArgs []string
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
	if _, err = os.Stat(resolvedPath); err == nil {
		return errors.New(serviceAlreadyExistsWarn(resolvedPath))
	}

	var buffer bytes.Buffer
	err = tmpl.Execute(&buffer, args)
	if err != nil {
		return fmt.Errorf("error generating %s: %v", st.Path, err)
	}
	fileMode := os.FileMode(0o644)
	if st.FileMode != 0 {
		fileMode = st.FileMode
	}

	plistFolder := filepath.Dir(resolvedPath)
	err = os.MkdirAll(plistFolder, 0o755)
	if err != nil {
		return fmt.Errorf("error creating %s: %v", plistFolder, err)
	}

	err = os.WriteFile(resolvedPath, buffer.Bytes(), fileMode)
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

func serviceAlreadyExistsWarn(service string) string {
	return fmt.Sprintf("cloudflared service is already installed at %s; if you are running a cloudflared tunnel, you "+
		"can point it to multiple origins, avoiding the need to run more than one cloudflared service in the "+
		"same machine; otherwise if you are really sure, you can do `cloudflared service uninstall` to clean "+
		"up the existing service and then try again this command",
		service,
	)
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

	output, _ := io.ReadAll(stderr)
	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("%s %v returned with error code %v due to: %v", command, args, err, string(output))
	}
	return nil
}
