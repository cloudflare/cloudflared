package diagnostic

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
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
	cliFlags        map[string]string
	icmpSources     []string
}

func NewDiagnosticHandler(
	log *zerolog.Logger,
	timeout time.Duration,
	systemCollector SystemCollector,
	tunnelID uuid.UUID,
	connectorID uuid.UUID,
	tracker *tunnelstate.ConnTracker,
	cliFlags map[string]string,
	icmpSources []string,
) *Handler {
	logger := log.With().Logger()
	if timeout == 0 {
		timeout = defaultCollectorTimeout
	}

	cliFlags[configurationKeyUID] = strconv.Itoa(os.Getuid())
	return &Handler{
		log:             &logger,
		timeout:         timeout,
		systemCollector: systemCollector,
		tunnelID:        tunnelID,
		connectorID:     connectorID,
		tracker:         tracker,
		cliFlags:        cliFlags,
		icmpSources:     icmpSources,
	}
}

func (handler *Handler) InstallEndpoints(router *http.ServeMux) {
	router.HandleFunc(cliConfigurationEndpoint, handler.ConfigurationHandler)
	router.HandleFunc(tunnelStateEndpoint, handler.TunnelStateHandler)
	router.HandleFunc(systemInformationEndpoint, handler.SystemHandler)
}

type SystemInformationResponse struct {
	Info *SystemInformation `json:"info"`
	Err  error              `json:"errors"`
}

func (handler *Handler) SystemHandler(writer http.ResponseWriter, request *http.Request) {
	logger := handler.log.With().Str(collectorField, systemCollectorName).Logger()
	logger.Info().Msg("Collection started")

	defer logger.Info().Msg("Collection finished")

	ctx, cancel := context.WithTimeout(request.Context(), handler.timeout)

	defer cancel()

	info, err := handler.systemCollector.Collect(ctx)

	response := SystemInformationResponse{
		Info: info,
		Err:  err,
	}

	encoder := json.NewEncoder(writer)
	err = encoder.Encode(response)
	if err != nil {
		logger.Error().Err(err).Msgf("error occurred whilst serializing information")
		writer.WriteHeader(http.StatusInternalServerError)
	}
}

type TunnelState struct {
	TunnelID    uuid.UUID                           `json:"tunnelID,omitempty"`
	ConnectorID uuid.UUID                           `json:"connectorID,omitempty"`
	Connections []tunnelstate.IndexedConnectionInfo `json:"connections,omitempty"`
	ICMPSources []string                            `json:"icmp_sources,omitempty"`
}

func (handler *Handler) TunnelStateHandler(writer http.ResponseWriter, _ *http.Request) {
	log := handler.log.With().Str(collectorField, tunnelStateCollectorName).Logger()
	log.Info().Msg("Collection started")

	defer log.Info().Msg("Collection finished")

	body := TunnelState{
		handler.tunnelID,
		handler.connectorID,
		handler.tracker.GetActiveConnections(),
		handler.icmpSources,
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

	encoder := json.NewEncoder(writer)

	err := encoder.Encode(handler.cliFlags)
	if err != nil {
		handler.log.Error().Err(err).Msgf("error occurred whilst serializing response")
		writer.WriteHeader(http.StatusInternalServerError)
	}
}

func writeResponse(w http.ResponseWriter, bytes []byte, logger *zerolog.Logger) {
	bytesWritten, err := w.Write(bytes)
	if err != nil {
		logger.Error().Err(err).Msg("error occurred writing response")
	} else if bytesWritten != len(bytes) {
		logger.Error().Msgf("error incomplete write response %d/%d", bytesWritten, len(bytes))
	}
}
