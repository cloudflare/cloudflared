package main

import (
	"fmt"

	"gopkg.in/urfave/cli.v2"

	"github.com/cloudflare/cloudflared/hello"
)


func helloWorld(c *cli.Context) error {
	address := fmt.Sprintf(":%d", c.Int("port"))
	listener, err := hello.CreateTLSListener(address)
	if err != nil {
		return err
	}
	defer listener.Close()
	err = hello.StartHelloWorldServer(logger, listener, nil)
	return err
}
