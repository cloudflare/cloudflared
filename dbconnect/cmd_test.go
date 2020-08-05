package dbconnect

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/urfave/cli/v2"
)

func TestCmd(t *testing.T) {
	tests := [][]string{
		{"cloudflared", "db-connect", "--playground"},
		{"cloudflared", "db-connect", "--playground", "--hostname", "sql.mysite.com"},
		{"cloudflared", "db-connect", "--url", "sqlite3::memory:?cache=shared", "--insecure"},
		{"cloudflared", "db-connect", "--url", "sqlite3::memory:?cache=shared", "--hostname", "sql.mysite.com", "--auth-domain", "mysite.cloudflareaccess.com", "--application-aud", "aud"},
	}

	app := &cli.App{
		Name:     "cloudflared",
		Commands: []*cli.Command{Cmd()},
	}

	for _, test := range tests {
		assert.NoError(t, app.Run(test))
	}
}
