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
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/quicktunnelauth"
)

const httpTimeout = 15 * time.Second

const disclaimer = "Thank you for trying Cloudflare Tunnel. Doing so, without a Cloudflare account, is a quick way to experiment and try it out. However, be aware that these account-less Tunnels have no uptime guarantee, are subject to the Cloudflare Online Services Terms of Use (https://www.cloudflare.com/website-terms/), and Cloudflare reserves the right to investigate your use of Tunnels for violations of such terms. If you intend to use Tunnels in production you should use a pre-created named tunnel by following: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps"

const (
	quickTunnelProtectedRequestBody    = `{"auth_mode":"otp"}`
	quickTunnelHelloWorldOrigin        = "built-in Hello World server (managed by cloudflared)"
	quickTunnelConfiguredOrigin        = "configured ingress"
	quickTunnelMaxProvisioningResponse = 1 << 20 // 1 MiB
)

// buildQuickTunnelRequestBody returns the provisioning request body.
// It returns a non-empty JSON body with auth_mode: otp when protected mode
// is requested, otherwise an empty body for public mode.
func buildQuickTunnelRequestBody(isProtected bool) []byte {
	if !isProtected {
		return nil
	}

	return []byte(quickTunnelProtectedRequestBody)
}

func describeQuickTunnelLocalOrigin(c *cli.Context, hasConfiguredIngress bool) (string, error) {
	if hasConfiguredIngress {
		return quickTunnelConfiguredOrigin, nil
	}
	if c.IsSet(ingress.HelloWorldFlag) {
		return quickTunnelHelloWorldOrigin, nil
	}

	originURL, err := config.ValidateUrl(c, false)
	if err != nil {
		return "", fmt.Errorf("validate Quick Tunnel local origin URL: %w", err)
	}
	return originURL.String(), nil
}

func formatQuickTunnelAllowedRecipientCounts(emailAddresses, emailDomains int) string {
	if emailAddresses == 0 && emailDomains == 0 {
		return "none"
	}

	counts := make([]string, 0, 2)
	if emailAddresses > 0 {
		label := "addresses"
		if emailAddresses == 1 {
			label = "address"
		}
		counts = append(counts, fmt.Sprintf("%d %s", emailAddresses, label))
	}
	if emailDomains > 0 {
		label := "domain rules"
		if emailDomains == 1 {
			label = "domain rule"
		}
		counts = append(counts, fmt.Sprintf("%d %s", emailDomains, label))
	}
	return strings.Join(counts, ", ")
}

func normalizeQuickTunnelURL(hostname string) string {
	if strings.HasPrefix(hostname, "https://") {
		return hostname
	}
	return "https://" + hostname
}

func quickTunnelStartupLines(
	isProtected bool,
	quickTunnelURL string,
	localOrigin string,
	recipientPolicy *quicktunnelauth.QuickTunnelAuthRecipientPolicy,
) []string {
	if !isProtected {
		return []string{
			"Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):",
			quickTunnelURL,
		}
	}

	emailAddresses, emailDomains := recipientPolicy.AllowedRecipientCounts()
	return []string{
		"Your protected quick Tunnel has been created! Visit it at (it may take some time to be reachable):",
		quickTunnelURL,
		"Authentication: One-Time PIN (using Cloudflare Access)",
		"Allowed recipients: " + formatQuickTunnelAllowedRecipientCounts(emailAddresses, emailDomains),
		"Local origin: " + localOrigin,
	}
}

// RunQuickTunnel requests a tunnel from the specified service.
// We use this to power quick tunnels on trycloudflare.com, but the
// service is open-source and could be used by anyone.
func RunQuickTunnel(sc *subcommandContext) error {
	sc.log.Info().Msg(disclaimer)
	sc.log.Info().Msg("Requesting new quick Tunnel on trycloudflare.com...")

	allowedMail := sc.c.StringSlice(flags.AllowedMail)
	isProtected := len(allowedMail) > 0
	var recipientPolicy *quicktunnelauth.QuickTunnelAuthRecipientPolicy
	var localOrigin string
	if isProtected {
		var err error
		recipientPolicy, err = quicktunnelauth.NewQuickTunnelAuthRecipientPolicy(allowedMail)
		if err != nil {
			return fmt.Errorf("validate Quick Tunnel recipient policy: %w", err)
		}
		localOrigin, err = describeQuickTunnelLocalOrigin(sc.c, len(config.GetConfiguration().Ingress) > 0)
		if err != nil {
			return err
		}
	}

	client := http.Client{
		Transport: &http.Transport{
			TLSHandshakeTimeout:   httpTimeout,
			ResponseHeaderTimeout: httpTimeout,
		},
		Timeout: httpTimeout,
	}
	reqBody := buildQuickTunnelRequestBody(isProtected)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/tunnel", sc.c.String("quick-service")), bytes.NewReader(reqBody))
	if err != nil {
		return errors.Wrap(err, "failed to build quick tunnel request")
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("User-Agent", buildInfo.UserAgent())

	resp, err := client.Do(req) //nolint:gosec // The quick-service endpoint is an explicit configuration value, and this request intentionally provisions the tunnel there.
	if err != nil {
		return errors.Wrap(err, "failed to request quick Tunnel")
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := readQuickTunnelProvisioningResponse(resp.Body)
	if err != nil {
		return err
	}

	data, err := decodeQuickTunnelProvisioningResponse(resp.StatusCode, respBody)
	if err != nil {
		return err
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

	var quickTunnelAuthorizer connection.HTTPRequestAuthorizer
	if recipientPolicy != nil {
		stateManager, err := quicktunnelauth.NewQuickTunnelAuthStateManager(data.Result.Hostname)
		if err != nil {
			return fmt.Errorf("initialize Quick Tunnel authentication state: %w", err)
		}
		assertionValidator, err := quicktunnelauth.NewQuickTunnelAuthAssertionValidator()
		if err != nil {
			return fmt.Errorf("initialize Quick Tunnel assertion validator: %w", err)
		}
		defer assertionValidator.Close()
		sessionManager, err := quicktunnelauth.NewQuickTunnelAuthSessionManager()
		if err != nil {
			return fmt.Errorf("initialize Quick Tunnel session manager: %w", err)
		}
		quickTunnelAuthorizer, err = quicktunnelauth.NewQuickTunnelAuthHandlerWithAuthorization(
			stateManager,
			assertionValidator,
			sessionManager,
			recipientPolicy,
		)
		if err != nil {
			return fmt.Errorf("initialize Quick Tunnel authentication handler: %w", err)
		}
	}

	quickTunnelURL := normalizeQuickTunnelURL(data.Result.Hostname)
	cliutil.LogTable(sc.log, quickTunnelStartupLines(isProtected, quickTunnelURL, localOrigin, recipientPolicy))

	if !sc.c.IsSet(flags.Protocol) {
		_ = sc.c.Set(flags.Protocol, "quic")
	}

	// Override the number of connections used. Quick tunnels shouldn't be used for production usage,
	// so, use a single connection instead.
	_ = sc.c.Set(flags.HaConnections, "1")
	return StartServer(
		sc.c,
		buildInfo,
		&connection.TunnelProperties{
			Credentials:           credentials,
			QuickTunnelUrl:        data.Result.Hostname,
			QuickTunnelAuthorizer: quickTunnelAuthorizer,
		},
		sc.log,
	)
}

func readQuickTunnelProvisioningResponse(body io.Reader) ([]byte, error) {
	response, err := io.ReadAll(io.LimitReader(body, quickTunnelMaxProvisioningResponse+1))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read quick-tunnel response")
	}
	if len(response) > quickTunnelMaxProvisioningResponse {
		return nil, errors.New("quick tunnel provisioning response exceeds maximum size")
	}
	return response, nil
}

func decodeQuickTunnelProvisioningResponse(statusCode int, response []byte) (QuickTunnelResponse, error) {
	var data QuickTunnelResponse
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		if err := json.Unmarshal(response, &data); err == nil && len(data.Errors) > 0 {
			return QuickTunnelResponse{}, fmt.Errorf("quick tunnel provisioning failed with status %d: %s", statusCode, formatQuickTunnelErrors(data.Errors))
		}
		return QuickTunnelResponse{}, fmt.Errorf("quick tunnel provisioning failed with status %d", statusCode)
	}

	if err := json.Unmarshal(response, &data); err != nil {
		return QuickTunnelResponse{}, errors.Wrap(err, "failed to unmarshal quick Tunnel")
	}
	return data, nil
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
	Secret     []byte `json:"secret"` //nolint:gosec // The provisioning API returns this tunnel credential, which is required to start the newly created tunnel.
}
