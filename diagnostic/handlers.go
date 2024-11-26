package diagnostic

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tunnelstate"
)

type Handler struct {
	log               *zerolog.Logger
	timeout           time.Duration
	systemCollector   SystemCollector
	tunnelID          uuid.UUID
	connectorID       uuid.UUID
	tracker           *tunnelstate.ConnTracker
	cli               *cli.Context
	flagInclusionList []string
}

func NewDiagnosticHandler(
	log *zerolog.Logger,
	timeout time.Duration,
	systemCollector SystemCollector,
	tunnelID uuid.UUID,
	connectorID uuid.UUID,
	tracker *tunnelstate.ConnTracker,
	cli *cli.Context,
	flagInclusionList []string,
) *Handler {
	logger := log.With().Logger()
	if timeout == 0 {
		timeout = defaultCollectorTimeout
	}

	return &Handler{
		log:               &logger,
		timeout:           timeout,
		systemCollector:   systemCollector,
		tunnelID:          tunnelID,
		connectorID:       connectorID,
		tracker:           tracker,
		cli:               cli,
		flagInclusionList: flagInclusionList,
	}
}

func (handler *Handler) SystemHandler(writer http.ResponseWriter, request *http.Request) {
	logger := handler.log.With().Str(collectorField, systemCollectorName).Logger()
	logger.Info().Msg("Collection started")

	defer logger.Info().Msg("Collection finished")

	ctx, cancel := context.WithTimeout(request.Context(), handler.timeout)

	defer cancel()

	info, rawInfo, err := handler.systemCollector.Collect(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("error occurred whilst collecting system information")

		if rawInfo != "" {
			logger.Info().Msg("using raw information fallback")
			bytes := []byte(rawInfo)
			writeResponse(writer, bytes, &logger)
		} else {
			logger.Error().Msg("no raw information available")
			writer.WriteHeader(http.StatusInternalServerError)
		}

		return
	}

	if info == nil {
		logger.Error().Msgf("system information collection is nil")
		writer.WriteHeader(http.StatusInternalServerError)
	}

	encoder := json.NewEncoder(writer)

	err = encoder.Encode(info)
	if err != nil {
		logger.Error().Err(err).Msgf("error occurred whilst serializing information")
		writer.WriteHeader(http.StatusInternalServerError)
	}
}

type tunnelStateResponse struct {
	TunnelID    uuid.UUID                           `json:"tunnelID,omitempty"`
	ConnectorID uuid.UUID                           `json:"connectorID,omitempty"`
	Connections []tunnelstate.IndexedConnectionInfo `json:"connections,omitempty"`
}

func (handler *Handler) TunnelStateHandler(writer http.ResponseWriter, _ *http.Request) {
	log := handler.log.With().Str(collectorField, tunnelStateCollectorName).Logger()
	log.Info().Msg("Collection started")

	defer log.Info().Msg("Collection finished")

	body := tunnelStateResponse{
		handler.tunnelID,
		handler.connectorID,
		handler.tracker.GetActiveConnections(),
	}
	encoder := json.NewEncoder(writer)

	err := encoder.Encode(body)
	if err != nil {
		handler.log.Error().Err(err).Msgf("error occurred whilst serializing information")
		writer.WriteHeader(http.StatusInternalServerError)
	}
}

func (handler *Handler) ConfigurationHandler(writer http.ResponseWriter, _ *http.Request) {
	log := handler.log.With().Str(collectorField, configurationCollectorName).Logger()
	log.Info().Msg("Collection started")

	defer func() {
		log.Info().Msg("Collection finished")
	}()

	flagsNames := handler.cli.FlagNames()
	flags := make(map[string]string, len(flagsNames))

	for _, flag := range flagsNames {
		value := handler.cli.String(flag)

		// empty values are not relevant
		if value == "" {
			continue
		}

		// exclude flags that are sensitive
		isIncluded := handler.isFlagIncluded(flag)
		if !isIncluded {
			continue
		}

		switch flag {
		case logger.LogDirectoryFlag:
		case logger.LogFileFlag:
			{
				// the log directory may be relative to the instance thus it must be resolved
				absolute, err := filepath.Abs(value)
				if err != nil {
					handler.log.Error().Err(err).Msgf("could not convert %s path to absolute", flag)
				} else {
					flags[flag] = absolute
				}
			}
		default:
			flags[flag] = value
		}
	}

	// The UID is included to help the
	// diagnostic tool to understand
	// if this instance is managed or not.
	flags[configurationKeyUID] = strconv.Itoa(os.Getuid())
	encoder := json.NewEncoder(writer)

	err := encoder.Encode(flags)
	if err != nil {
		handler.log.Error().Err(err).Msgf("error occurred whilst serializing response")
		writer.WriteHeader(http.StatusInternalServerError)
	}
}

func (handler *Handler) isFlagIncluded(flag string) bool {
	isIncluded := false

	for _, include := range handler.flagInclusionList {
		if include == flag {
			isIncluded = true

			break
		}
	}

	return isIncluded
}

func writeResponse(w http.ResponseWriter, bytes []byte, logger *zerolog.Logger) {
	bytesWritten, err := w.Write(bytes)
	if err != nil {
		logger.Error().Err(err).Msg("error occurred writing response")
	} else if bytesWritten != len(bytes) {
		logger.Error().Msgf("error incomplete write response %d/%d", bytesWritten, len(bytes))
	}
}
