package cliutil

import (
	"fmt"

	"github.com/urfave/cli/v2"
)

func RemovedCommand(name string) *cli.Command {
	return &cli.Command{
		Name: name,
		Action: func(context *cli.Context) error {
			return cli.Exit(
				fmt.Sprintf("%s command is no longer supported by cloudflared. Consult Argo Tunnel documentation for possible alternative solutions.", name),
				-1,
			)
		},
		Description: fmt.Sprintf("%s is deprecated", name),
		Hidden:      true,
	}
}
