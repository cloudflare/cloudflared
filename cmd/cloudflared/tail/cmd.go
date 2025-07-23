package tail

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/mattn/go-colorable"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"nhooyr.io/websocket"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	cfdflags "github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
	"github.com/cloudflare/cloudflared/credentials"
	"github.com/cloudflare/cloudflared/management"
)

var buildInfo *cliutil.BuildInfo

func Init(bi *cliutil.BuildInfo) {
	buildInfo = bi
}

func Command() *cli.Command {
	subcommands := []*cli.Command{
		buildTailManagementTokenSubcommand(),
	}

	return buildTailCommand(subcommands)
}

func buildTailManagementTokenSubcommand() *cli.Command {
	return &cli.Command{
		Name:        "token",
		Action:      cliutil.ConfiguredAction(managementTokenCommand),
		Usage:       "Get management access jwt",
		UsageText:   "cloudflared tail token TUNNEL_ID",
		Description: `Get management access jwt for a tunnel`,
		Hidden:      true,
	}
}

func managementTokenCommand(c *cli.Context) error {
	log := createLogger(c)

	token, err := getManagementToken(c, log)
	if err != nil {
		return err
	}
	tokenResponse := struct {
		Token string `json:"token"`
	}{Token: token}

	return json.NewEncoder(os.Stdout).Encode(tokenResponse)
}

func buildTailCommand(subcommands []*cli.Command) *cli.Command {
	return &cli.Command{
		Name:      "tail",
		Action:    Run,
		Usage:     "Stream logs from a remote cloudflared",
		UsageText: "cloudflared tail [tail command options] [TUNNEL-ID]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "connector-id",
				Usage:   "Access a specific cloudflared instance by connector id (for when a tunnel has multiple cloudflared's)",
				Value:   "",
				EnvVars: []string{"TUNNEL_MANAGEMENT_CONNECTOR"},
			},
			&cli.StringSliceFlag{
				Name:    "event",
				Usage:   "Filter by specific Events (cloudflared, http, tcp, udp) otherwise, defaults to send all events",
				EnvVars: []string{"TUNNEL_MANAGEMENT_FILTER_EVENTS"},
			},
			&cli.StringFlag{
				Name:    "level",
				Usage:   "Filter by specific log levels (debug, info, warn, error). Filters by debug log level by default.",
				EnvVars: []string{"TUNNEL_MANAGEMENT_FILTER_LEVEL"},
				Value:   "debug",
			},
			&cli.Float64Flag{
				Name:    "sample",
				Usage:   "Sample log events by percentage (0.0 .. 1.0). No sampling by default.",
				EnvVars: []string{"TUNNEL_MANAGEMENT_FILTER_SAMPLE"},
				Value:   1.0,
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "Access token for a specific tunnel",
				Value:   "",
				EnvVars: []string{"TUNNEL_MANAGEMENT_TOKEN"},
			},
			&cli.StringFlag{
				Name:    cfdflags.ManagementHostname,
				Usage:   "Management hostname to signify incoming management requests",
				EnvVars: []string{"TUNNEL_MANAGEMENT_HOSTNAME"},
				Hidden:  true,
				Value:   "management.argotunnel.com",
			},
			&cli.StringFlag{
				Name:   "trace",
				Usage:  "Set a cf-trace-id for the request",
				Hidden: true,
				Value:  "",
			},
			&cli.StringFlag{
				Name:    cfdflags.LogLevel,
				Value:   "info",
				Usage:   "Application logging level {debug, info, warn, error, fatal}",
				EnvVars: []string{"TUNNEL_LOGLEVEL"},
			},
			&cli.StringFlag{
				Name:    cfdflags.OriginCert,
				Usage:   "Path to the certificate generated for your origin when you run cloudflared login.",
				EnvVars: []string{"TUNNEL_ORIGIN_CERT"},
				Value:   credentials.FindDefaultOriginCertPath(),
			},
			cliutil.FlagLogOutput,
		},
		Subcommands: subcommands,
	}
}

// Middleware validation error struct for returning to the eyeball
type managementError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Middleware validation error HTTP response JSON for returning to the eyeball
type managementErrorResponse struct {
	Success bool              `json:"success,omitempty"`
	Errors  []managementError `json:"errors,omitempty"`
}

func handleValidationError(resp *http.Response, log *zerolog.Logger) {
	if resp.StatusCode == 530 {
		log.Error().Msgf("no cloudflared connector available or reachable via management request (a recent version of cloudflared is required to use streaming logs)")
	}
	var managementErr managementErrorResponse
	err := json.NewDecoder(resp.Body).Decode(&managementErr)
	if err != nil {
		log.Error().Msgf("unable to start management log streaming session: http response code returned %d", resp.StatusCode)
		return
	}
	if managementErr.Success || len(managementErr.Errors) == 0 {
		log.Error().Msgf("management tunnel validation returned success with invalid HTTP response code to convert to a WebSocket request")
		return
	}
	for _, e := range managementErr.Errors {
		log.Error().Msgf("management request failed validation: (%d) %s", e.Code, e.Message)
	}
}

// logger will be created to emit only against the os.Stderr as to not obstruct with normal output from
// management requests
func createLogger(c *cli.Context) *zerolog.Logger {
	level, levelErr := zerolog.ParseLevel(c.String(cfdflags.LogLevel))
	if levelErr != nil {
		level = zerolog.InfoLevel
	}
	var writer io.Writer
	switch c.String(cfdflags.LogFormatOutput) {
	case cfdflags.LogFormatOutputValueJSON:
		// zerolog by default outputs as JSON
		writer = os.Stderr
	case cfdflags.LogFormatOutputValueDefault:
		// "default" and unset use the same logger output format
		fallthrough
	default:
		writer = zerolog.ConsoleWriter{
			Out:        colorable.NewColorable(os.Stderr),
			TimeFormat: time.RFC3339,
		}
	}
	log := zerolog.New(writer).With().Timestamp().Logger().Level(level)
	return &log
}

// parseFilters will attempt to parse provided filters to send to with the EventStartStreaming
func parseFilters(c *cli.Context) (*management.StreamingFilters, error) {
	var level *management.LogLevel
	var sample float64

	events := make([]management.LogEventType, 0)

	argLevel := c.String("level")
	argEvents := c.StringSlice("event")
	argSample := c.Float64("sample")

	if argLevel != "" {
		l, ok := management.ParseLogLevel(argLevel)
		if !ok {
			return nil, fmt.Errorf("invalid --level filter provided, please use one of the following Log Levels: debug, info, warn, error")
		}
		level = &l
	}

	for _, v := range argEvents {
		t, ok := management.ParseLogEventType(v)
		if !ok {
			return nil, fmt.Errorf("invalid --event filter provided, please use one of the following EventTypes: cloudflared, http, tcp, udp")
		}
		events = append(events, t)
	}

	if argSample <= 0.0 || argSample > 1.0 {
		return nil, fmt.Errorf("invalid --sample value provided, please make sure it is in the range (0.0 .. 1.0)")
	}
	sample = argSample

	if level == nil && len(events) == 0 && argSample != 1.0 {
		// When no filters are provided, do not return a StreamingFilters struct
		return nil, nil
	}

	return &management.StreamingFilters{
		Level:    level,
		Events:   events,
		Sampling: sample,
	}, nil
}

// getManagementToken will make a call to the Cloudflare API to acquire a management token for the requested tunnel.
func getManagementToken(c *cli.Context, log *zerolog.Logger) (string, error) {
	userCreds, err := credentials.Read(c.String(cfdflags.OriginCert), log)
	if err != nil {
		return "", err
	}

	var apiURL string
	if userCreds.IsFEDEndpoint() {
		apiURL = credentials.FedRampBaseApiURL
	} else {
		apiURL = c.String(cfdflags.ApiURL)
	}

	client, err := userCreds.Client(apiURL, buildInfo.UserAgent(), log)
	if err != nil {
		return "", err
	}

	tunnelIDString := c.Args().First()
	if tunnelIDString == "" {
		return "", errors.New("no tunnel ID provided")
	}
	tunnelID, err := uuid.Parse(tunnelIDString)
	if err != nil {
		return "", errors.New("unable to parse provided tunnel id as a valid UUID")
	}

	token, err := client.GetManagementToken(tunnelID)
	if err != nil {
		return "", err
	}

	return token, nil
}

// buildURL will build the management url to contain the required query parameters to authenticate the request.
func buildURL(c *cli.Context, log *zerolog.Logger) (url.URL, error) {
	var err error

	token := c.String("token")
	if token == "" {
		token, err = getManagementToken(c, log)
		if err != nil {
			return url.URL{}, fmt.Errorf("unable to acquire management token for requested tunnel id: %w", err)
		}
	}

	claims, err := management.ParseToken(token)
	if err != nil {
		return url.URL{}, fmt.Errorf("failed to determine if token is FED: %w", err)
	}

	var managementHostname string
	if claims.IsFed() {
		managementHostname = credentials.FedRampHostname
	} else {
		managementHostname = c.String(cfdflags.ManagementHostname)
	}

	query := url.Values{}
	query.Add("access_token", token)
	connector := c.String("connector-id")
	if connector != "" {
		connectorID, err := uuid.Parse(connector)
		if err != nil {
			return url.URL{}, fmt.Errorf("unabled to parse 'connector-id' flag into a valid UUID: %w", err)
		}
		query.Add("connector_id", connectorID.String())
	}
	return url.URL{Scheme: "wss", Host: managementHostname, Path: "/logs", RawQuery: query.Encode()}, nil
}

func printLine(log *management.Log, logger *zerolog.Logger) {
	fields, err := json.Marshal(log.Fields)
	if err != nil {
		fields = []byte("unable to parse fields")
		logger.Debug().Msgf("unable to parse fields from event %+v", log)
	}
	fmt.Printf("%s %s %s %s %s\n", log.Time, log.Level, log.Event, log.Message, fields)
}

func printJSON(log *management.Log, logger *zerolog.Logger) {
	output, err := json.Marshal(log)
	if err != nil {
		logger.Debug().Msgf("unable to parse event to json %+v", log)
	} else {
		fmt.Println(string(output))
	}
}

// Run implements a foreground runner
func Run(c *cli.Context) error {
	log := createLogger(c)

	signals := make(chan os.Signal, 10)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)

	output := "default"
	switch c.String("output") {
	case "default", "":
		output = "default"
	case "json":
		output = "json"
	default:
		log.Err(errors.New("invalid --output value provided, please make sure it is one of: default, json")).Send()
	}

	filters, err := parseFilters(c)
	if err != nil {
		log.Error().Err(err).Msgf("invalid filters provided")
		return nil
	}

	u, err := buildURL(c, log)
	if err != nil {
		log.Err(err).Msg("unable to construct management request URL")
		return nil
	}

	header := make(http.Header)
	header.Add("User-Agent", buildInfo.UserAgent())
	trace := c.String("trace")
	if trace != "" {
		header["cf-trace-id"] = []string{trace}
	}
	ctx := c.Context
	// nolint: bodyclose
	conn, resp, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			handleValidationError(resp, log)
			return nil
		}
		log.Error().Err(err).Msgf("unable to start management log streaming session")
		return nil
	}
	defer conn.Close(websocket.StatusInternalError, "management connection was closed abruptly")

	// Once connection is established, send start_streaming event to begin receiving logs
	err = management.WriteEvent(conn, ctx, &management.EventStartStreaming{
		ClientEvent: management.ClientEvent{Type: management.StartStreaming},
		Filters:     filters,
	})
	if err != nil {
		log.Error().Err(err).Msg("unable to request logs from management tunnel")
		return nil
	}
	log.Debug().
		Str("tunnel-id", c.Args().First()).
		Str("connector-id", c.String("connector-id")).
		Interface("filters", filters).
		Msg("connected")

	readerDone := make(chan struct{})

	go func() {
		defer close(readerDone)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				event, err := management.ReadServerEvent(conn, ctx)
				if err != nil {
					if closeErr := management.AsClosed(err); closeErr != nil {
						// If the client (or the server) already closed the connection, don't continue to
						// attempt to read from the client.
						if closeErr.Code == websocket.StatusNormalClosure {
							return
						}
						// Only log abnormal closures
						log.Error().Msgf("received remote closure: (%d) %s", closeErr.Code, closeErr.Reason)
						return
					}
					log.Err(err).Msg("unable to read event from server")
					return
				}
				switch event.Type {
				case management.Logs:
					logs, ok := management.IntoServerEvent(event, management.Logs)
					if !ok {
						log.Error().Msgf("invalid logs event")
						continue
					}
					// Output all the logs received to stdout
					for _, l := range logs.Logs {
						if output == "json" {
							printJSON(l, log)
						} else {
							printLine(l, log)
						}
					}
				case management.UnknownServerEventType:
					fallthrough
				default:
					log.Debug().Msgf("unexpected log event type: %s", event.Type)
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-readerDone:
			return nil
		case <-signals:
			log.Debug().Msg("closing management connection")
			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			conn.Close(websocket.StatusNormalClosure, "")
			select {
			case <-readerDone:
			case <-time.After(time.Second):
			}
			return nil
		}
	}
}
