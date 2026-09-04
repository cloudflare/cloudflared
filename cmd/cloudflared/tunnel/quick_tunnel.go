package tunnel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
	"github.com/cloudflare/cloudflared/connection"
)

const httpTimeout = 15 * time.Second

const disclaimer = "Thank you for trying Cloudflare Tunnel. Doing so, without a Cloudflare account, is a quick way to experiment and try it out. However, be aware that these account-less Tunnels have no uptime guarantee, are subject to the Cloudflare Online Services Terms of Use (https://www.cloudflare.com/website-terms/), and Cloudflare reserves the right to investigate your use of Tunnels for violations of such terms. If you intend to use Tunnels in production you should use a pre-created named tunnel by following: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps"

const (
	quickTunnelAuthModeField = "auth_mode"
	quickTunnelAuthModeOTP   = "otp"
)

// buildQuickTunnelRequestBody returns the provisioning request body.
// It returns a non-empty JSON body with auth_mode: otp when protected mode
// is requested, otherwise an empty body for public mode.
func buildQuickTunnelRequestBody(isProtected bool) ([]byte, error) {
	if !isProtected {
		return nil, nil
	}

	return json.Marshal(map[string]string{quickTunnelAuthModeField: quickTunnelAuthModeOTP})
}

// RunQuickTunnel requests a tunnel from the specified service.
// We use this to power quick tunnels on trycloudflare.com, but the
// service is open-source and could be used by anyone.
func RunQuickTunnel(sc *subcommandContext) error {
	sc.log.Info().Msg(disclaimer)
	sc.log.Info().Msg("Requesting new quick Tunnel on trycloudflare.com...")

	// TODO(TUN-10798): register the --allowed-mail flag so this path becomes reachable.
	allowedMail := sc.c.StringSlice(flags.AllowedMail)
	isProtected := len(allowedMail) > 0

	client := http.Client{
		Transport: &http.Transport{
			TLSHandshakeTimeout:   httpTimeout,
			ResponseHeaderTimeout: httpTimeout,
		},
		Timeout: httpTimeout,
	}

	reqBody, err := buildQuickTunnelRequestBody(isProtected)
	if err != nil {
		return errors.Wrap(err, "failed to build quick tunnel request body")
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/tunnel", sc.c.String("quick-service")), bytes.NewReader(reqBody))
	if err != nil {
		return errors.Wrap(err, "failed to build quick tunnel request")
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("User-Agent", buildInfo.UserAgent())

	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to request quick Tunnel")
	}
	defer func() { _ = resp.Body.Close() }()

	// This will read the entire response into memory so we can print it in case of error
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "failed to read quick-tunnel response")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var data QuickTunnelResponse
		if err := json.Unmarshal(respBody, &data); err == nil && len(data.Errors) > 0 {
			return fmt.Errorf("quick tunnel provisioning failed with status %d: %s", resp.StatusCode, formatQuickTunnelErrors(data.Errors))
		}
		return fmt.Errorf("quick tunnel provisioning failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var data QuickTunnelResponse
	if err := json.Unmarshal(respBody, &data); err != nil {
		respString := string(respBody)
		fields := map[string]interface{}{"status_code": resp.Status}
		sc.log.Err(err).Fields(fields).Msgf("Error unmarshaling QuickTunnel response: %s", respString)
		return errors.Wrap(err, "failed to unmarshal quick Tunnel")
	}

	// TODO(TUN-10791): Add CLI-level coverage that provisioning errors are logged to users.
	if len(data.Errors) > 0 {
		return fmt.Errorf("quick tunnel provisioning failed: %s", formatQuickTunnelErrors(data.Errors))
	}

	if !data.Success {
		return errors.New("quick tunnel provisioning failed")
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

	cliutil.LogTable(sc.log, []string{
		"Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):",
		url,
	})

	if !sc.c.IsSet(flags.Protocol) {
		_ = sc.c.Set(flags.Protocol, "auto")
	}

	// Override the number of connections used. Quick tunnels shouldn't be used for production usage,
	// so, use a single connection instead.
	_ = sc.c.Set(flags.HaConnections, "1")
	return StartServer(
		sc.c,
		buildInfo,
		&connection.TunnelProperties{Credentials: credentials, QuickTunnelUrl: data.Result.Hostname, IsProtected: isProtected},
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

func formatQuickTunnelErrors(errors []QuickTunnelError) string {
	messages := make([]string, len(errors))
	for i, e := range errors {
		messages[i] = fmt.Sprintf("[%d] %s", e.Code, e.Message)
	}
	return strings.Join(messages, "; ")
}

type QuickTunnel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	AccountTag string `json:"account_tag"`
	Secret     []byte `json:"secret"`
}
