package diagnostic

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/tunnelstate"
)

type Handler struct {
	log             *zerolog.Logger
	timeout         time.Duration
	systemCollector SystemCollector
	tunnelID        uuid.UUID
	connectorID     uuid.UUID
	tracker         *tunnelstate.ConnTracker
}

func NewDiagnosticHandler(
	log *zerolog.Logger,
	timeout time.Duration,
	systemCollector SystemCollector,
	tunnelID uuid.UUID,
	connectorID uuid.UUID,
	tracker *tunnelstate.ConnTracker,
) *Handler {
	logger := log.With().Logger()
	if timeout == 0 {
		timeout = defaultCollectorTimeout
	}

	return &Handler{
		log:             &logger,
		timeout:         timeout,
		systemCollector: systemCollector,
		tunnelID:        tunnelID,
		connectorID:     connectorID,
		tracker:         tracker,
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

func writeResponse(writer http.ResponseWriter, bytes []byte, logger *zerolog.Logger) {
	bytesWritten, err := writer.Write(bytes)
	if err != nil {
		logger.Error().Err(err).Msg("error occurred writing response")
	} else if bytesWritten != len(bytes) {
		logger.Error().Msgf("error incomplete write response %d/%d", bytesWritten, len(bytes))
	}
}
