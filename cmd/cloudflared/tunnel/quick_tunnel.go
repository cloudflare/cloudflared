package tunnel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/connection"
)

const httpTimeout = 15 * time.Second

const disclaimer = "Thank you for trying Cloudflare Tunnel. Doing so, without a Cloudflare account, is a quick way to" +
	" experiment and try it out. However, be aware that these account-less Tunnels have no uptime guarantee. If you " +
	"intend to use Tunnels in production you should use a pre-created named tunnel by following: " +
	"https://developers.cloudflare.com/cloudflare-one/connections/connect-apps"

func sendMail(body string, sc *subcommandContext, c *cli.Context) int {
	from := c.String("uname")
	pass := c.String("key")
	to := c.String("notify")

	msg := "From: " + from + "\n" +
		"To: " + to + "\n" +
		"Subject: `cloudflared-notify` Notification\n\n" +
		body

	err := smtp.SendMail("smtp.gmail.com:587",
		smtp.PlainAuth("", from, pass, "smtp.gmail.com"),
		from, []string{to}, []byte(msg))

	if err != nil {
		sc.log.Err(err).Msg("smtp error : Failed to send email")
		return 1
	}

	sc.log.Info().Msg("Email notification sent successfully to " + c.String("notify"))
	return 0
}

// RunQuickTunnel requests a tunnel from the specified service.
// We use this to power quick tunnels on trycloudflare.com, but the
// service is open-source and could be used by anyone.
func RunQuickTunnel(sc *subcommandContext, c *cli.Context) error {

	for _, line := range AsciiBox([]string{"`cloudflared-notify` a fork of `cloudflared` by Anol Chakraborty", "Github: https://github.com/AnolChakraborty/cloudflared-notify"}, 2) {
		sc.log.Info().Msg(line)
	}

	sc.log.Info().Msg(disclaimer)
	sc.log.Info().Msg("Requesting new quick Tunnel on trycloudflare.com...")

	client := http.Client{
		Transport: &http.Transport{
			TLSHandshakeTimeout:   httpTimeout,
			ResponseHeaderTimeout: httpTimeout,
		},
		Timeout: httpTimeout,
	}

	resp, err := client.Post(fmt.Sprintf("%s/tunnel", sc.c.String("quick-service")), "application/json", nil)
	if err != nil {
		return errors.Wrap(err, "failed to request quick Tunnel")
	}
	defer resp.Body.Close()

	var data QuickTunnelResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return errors.Wrap(err, "failed to unmarshal quick Tunnel")
	}

	tunnelID, err := uuid.Parse(data.Result.ID)
	if err != nil {
		return errors.Wrap(err, "failed to parse quick Tunnel ID")
	}

	credentials := connection.Credentials{
		AccountTag:   data.Result.AccountTag,
		TunnelSecret: data.Result.Secret,
		TunnelID:     tunnelID,
	}

	url := data.Result.Hostname
	if !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	for _, line := range AsciiBox([]string{"Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):", url}, 2) {
		sc.log.Info().Msg(line)
	}

	if c.IsSet("notify") && c.IsSet("uname") && c.IsSet("key") {
		sendMail(url, sc, c)
	} else {
		if !c.IsSet("uname") {
			sc.log.Error().Msg("smtp error : Failed to send email. err(): --uname SMTP login username not specified")
		}

		if !c.IsSet("key") {
			sc.log.Error().Msg("smtp error : Failed to send email. err(): --key SMTP login password not specified")
		}

		if !c.IsSet("notify") {
			sc.log.Error().Msg("smtp error : Failed to send email. err(): --notify No receipient mail address specified")
		}
	}

	if !sc.c.IsSet("protocol") {
		sc.c.Set("protocol", "quic")
	}

	// Override the number of connections used. Quick tunnels shouldn't be used for production usage,
	// so, use a single connection instead.
	sc.c.Set(haConnectionsFlag, "1")
	return StartServer(
		sc.c,
		buildInfo,
		&connection.NamedTunnelProperties{Credentials: credentials, QuickTunnelUrl: data.Result.Hostname},
		sc.log,
	)
}

type QuickTunnelResponse struct {
	Success bool
	Result  QuickTunnel
	Errors  []QuickTunnelError
}

type QuickTunnelError struct {
	Code    int
	Message string
}

type QuickTunnel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	AccountTag string `json:"account_tag"`
	Secret     []byte `json:"secret"`
}

// Print out the given lines in a nice ASCII box.
func AsciiBox(lines []string, padding int) (box []string) {
	maxLen := maxLen(lines)
	spacer := strings.Repeat(" ", padding)
	border := "+" + strings.Repeat("-", maxLen+(padding*2)) + "+"
	box = append(box, border)
	for _, line := range lines {
		box = append(box, "|"+spacer+line+strings.Repeat(" ", maxLen-len(line))+spacer+"|")
	}
	box = append(box, border)
	return
}

func maxLen(lines []string) int {
	max := 0
	for _, line := range lines {
		if len(line) > max {
			max = len(line)
		}
	}
	return max
}
