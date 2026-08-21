package tunnel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"rsc.io/qr"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
	"github.com/cloudflare/cloudflared/connection"
)

const httpTimeout = 15 * time.Second

// qrQuietZoneModules is the number of empty modules added around the rendered
// QR code. Four modules is the minimum quiet zone required by the QR spec.
const qrQuietZoneModules = 4

const disclaimer = "Thank you for trying Cloudflare Tunnel. Doing so, without a Cloudflare account, is a quick way to experiment and try it out. However, be aware that these account-less Tunnels have no uptime guarantee, are subject to the Cloudflare Online Services Terms of Use (https://www.cloudflare.com/website-terms/), and Cloudflare reserves the right to investigate your use of Tunnels for violations of such terms. If you intend to use Tunnels in production you should use a pre-created named tunnel by following: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps"

// RunQuickTunnel requests a tunnel from the specified service.
// We use this to power quick tunnels on trycloudflare.com, but the
// service is open-source and could be used by anyone.
func RunQuickTunnel(sc *subcommandContext) error {
	sc.log.Info().Msg(disclaimer)
	sc.log.Info().Msg("Requesting new quick Tunnel on trycloudflare.com...")

	client := http.Client{
		Transport: &http.Transport{
			TLSHandshakeTimeout:   httpTimeout,
			ResponseHeaderTimeout: httpTimeout,
		},
		Timeout: httpTimeout,
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/tunnel", sc.c.String("quick-service")), nil)
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
	rsp_body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "failed to read quick-tunnel response")
	}

	var data QuickTunnelResponse
	if err := json.Unmarshal(rsp_body, &data); err != nil {
		rsp_string := string(rsp_body)
		fields := map[string]interface{}{"status_code": resp.Status}
		sc.log.Err(err).Fields(fields).Msgf("Error unmarshaling QuickTunnel response: %s", rsp_string)
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

	cliutil.LogTable(sc.log, quickTunnelURLDisplayLines(data.Result.Hostname))

	quickTunnelQRLines, err := quickTunnelQRCodeLines(data.Result.Hostname)
	if err != nil {
		sc.log.Warn().Err(err).Msg("Failed to generate quick Tunnel QR code")
	} else {
		// Filter out the all-white quiet-zone rows so the terminal output
		// stays compact while the QR code itself remains scannable.
		for _, line := range quickTunnelQRLines {
			if line != "" {
				sc.log.Info().Msg(line)
			}
		}
		sc.log.Info().Msg("")
	}

	if !sc.c.IsSet(flags.Protocol) {
		_ = sc.c.Set(flags.Protocol, "quic")
	}

	// Override the number of connections used. Quick tunnels shouldn't be used for production usage,
	// so, use a single connection instead.
	_ = sc.c.Set(flags.HaConnections, "1")
	return StartServer(
		sc.c,
		buildInfo,
		&connection.TunnelProperties{Credentials: credentials, QuickTunnelUrl: data.Result.Hostname},
		sc.log,
	)
}

func quickTunnelURLDisplayLines(hostname string) []string {
	return []string{
		"Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):",
		normalizeQuickTunnelURL(hostname),
	}
}

func quickTunnelQRCodeLines(hostname string) ([]string, error) {
	url := normalizeQuickTunnelURL(hostname)
	code, err := qr.Encode(url, qr.L)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create quick Tunnel QR code")
	}

	return renderHalfBlockQRCode(code, qrQuietZoneModules), nil
}

func renderHalfBlockQRCode(code *qr.Code, quietZone int) []string {
	minX, minY, maxX, maxY := code.Size, code.Size, 0, 0
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				minX = min(minX, x)
				minY = min(minY, y)
				maxX = max(maxX, x)
				maxY = max(maxY, y)
			}
		}
	}

	minX -= quietZone
	minY -= quietZone
	maxX += quietZone
	maxY += quietZone

	lines := make([]string, 0, ((maxY-minY)+2)/2)
	lineWidth := maxX - minX + 1
	for y := minY; y <= maxY; y += 2 {
		var line strings.Builder
		line.Grow(lineWidth)
		for x := minX; x <= maxX; x++ {
			top := code.Black(x, y)
			bottom := y+1 <= maxY && code.Black(x, y+1)
			switch {
			case top && bottom:
				line.WriteRune('█')
			case top:
				line.WriteRune('▀')
			case bottom:
				line.WriteRune('▄')
			default:
				line.WriteRune(' ')
			}
		}
		lines = append(lines, line.String())
	}
	return lines
}

func normalizeQuickTunnelURL(hostname string) string {
	if strings.HasPrefix(hostname, "https://") {
		return hostname
	}
	return "https://" + hostname
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
