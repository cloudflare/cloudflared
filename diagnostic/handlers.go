package diagnostic

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/prechecks"
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
	precheckReports *PrecheckReportStore
}

// HandlerOption configures a diagnostic Handler.
type HandlerOption func(*Handler)

// PrecheckReportStore safely shares a completed precheck report between the
// startup precheck and diagnostic HTTP goroutines.
type PrecheckReportStore struct {
	mu     sync.RWMutex
	report *prechecks.Report
}

// NewPrecheckReportStore creates an empty precheck report store.
func NewPrecheckReportStore() *PrecheckReportStore {
	return &PrecheckReportStore{}
}

// WithPrecheckReportStore configures the report store used by /diag/precheck.
func WithPrecheckReportStore(store *PrecheckReportStore) HandlerOption {
	return func(handler *Handler) {
		if store != nil {
			handler.precheckReports = store
		}
	}
}

// Set publishes a completed startup precheck report.
func (store *PrecheckReportStore) Set(report prechecks.Report) {
	report.Results = append([]prechecks.CheckResult(nil), report.Results...)
	store.mu.Lock()
	store.report = &report
	store.mu.Unlock()
}

func (store *PrecheckReportStore) get() (prechecks.Report, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if store.report == nil {
		return prechecks.Report{}, false
	}

	report := *store.report
	report.Results = append([]prechecks.CheckResult(nil), report.Results...)
	return report, true
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
	options ...HandlerOption,
) *Handler {
	logger := log.With().Logger()
	if timeout == 0 {
		timeout = defaultCollectorTimeout
	}

	cliFlags[configurationKeyUID] = strconv.Itoa(os.Getuid())
	handler := &Handler{
		log:             &logger,
		timeout:         timeout,
		systemCollector: systemCollector,
		tunnelID:        tunnelID,
		connectorID:     connectorID,
		tracker:         tracker,
		cliFlags:        cliFlags,
		icmpSources:     icmpSources,
		precheckReports: NewPrecheckReportStore(),
	}
	for _, option := range options {
		option(handler)
	}
	return handler
}

func (handler *Handler) InstallEndpoints(router *http.ServeMux) {
	router.HandleFunc(cliConfigurationEndpoint, handler.ConfigurationHandler)
	router.HandleFunc(tunnelStateEndpoint, handler.TunnelStateHandler)
	router.HandleFunc(systemInformationEndpoint, handler.SystemHandler)
	router.HandleFunc(precheckEndpoint, handler.PrecheckHandler)
}

type PrecheckResult struct {
	Component string `json:"component"`
	Target    string `json:"target"`
	Status    string `json:"status"`
	Details   string `json:"details"`
	Action    string `json:"action,omitempty"`
}

type PrecheckResponse struct {
	RunID             uuid.UUID        `json:"run_id"`
	Results           []PrecheckResult `json:"results"`
	HardFail          bool             `json:"hard_fail"`
	SuggestedProtocol string           `json:"suggested_protocol,omitempty"`
}

func (handler *Handler) PrecheckHandler(writer http.ResponseWriter, _ *http.Request) {
	report, ok := handler.precheckReports.get()
	if !ok {
		writer.WriteHeader(http.StatusNoContent)
		return
	}

	response := PrecheckResponse{
		RunID:    report.RunID,
		Results:  make([]PrecheckResult, 0, len(report.Results)),
		HardFail: report.HasHardFail(),
	}
	if report.SuggestedProtocol != nil {
		response.SuggestedProtocol = report.SuggestedProtocol.String()
	}
	for _, result := range report.Results {
		response.Results = append(response.Results, PrecheckResult{
			Component: result.Component,
			Target:    result.Target,
			Status:    strings.ToLower(result.ProbeStatus.String()),
			Details:   result.Details,
			Action:    result.Action,
		})
	}

	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		handler.log.Error().Err(err).Msg("error occurred whilst serializing precheck report")
	}
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
