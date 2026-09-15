package proxy

import (
	"net/http"
	"strconv"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/management"
)

const (
	logFieldCFRay         = "cfRay"
	logFieldLBProbe       = "lbProbe"
	logFieldRule          = "ingressRule"
	logFieldOriginService = "originService"
	logFieldConnIndex     = "connIndex"
	logFieldDestAddr      = "destAddr"
)

var (
	LogFieldFlowID = "flowID"
)

// newHTTPLogger creates a child zerolog.Logger from the provided with added context from the HTTP request, ingress
// services, and connection index.
func newHTTPLogger(logger *zerolog.Logger, connIndex uint8, req *http.Request, rule int, serviceName string) zerolog.Logger {
	ctx := logger.With().
		Int(management.EventTypeKey, int(management.HTTP)).
		Uint8(logFieldConnIndex, connIndex)
	cfRay := connection.FindCfRayHeader(req)
	lbProbe := connection.IsLBProbeRequest(req)
	if cfRay != "" {
		ctx.Str(logFieldCFRay, cfRay)
	}
	if lbProbe {
		ctx.Bool(logFieldLBProbe, lbProbe)
	}
	return ctx.
		Str(logFieldOriginService, serviceName).
		Interface(logFieldRule, rule).
		Logger()
}

// newTCPLogger creates a child zerolog.Logger from the provided with added context from the TCPRequest.
func newTCPLogger(logger *zerolog.Logger, req *connection.TCPRequest) zerolog.Logger {
	return logger.With().
		Int(management.EventTypeKey, int(management.TCP)).
		Uint8(logFieldConnIndex, req.ConnIndex).
		Str(logFieldOriginService, ingress.ServiceWarpRouting).
		Str(LogFieldFlowID, req.FlowID).
		Str(logFieldDestAddr, req.Dest).
		Uint8(logFieldConnIndex, req.ConnIndex).
		Logger()
}

// logHTTPRequest logs a Debug message with the corresponding HTTP request details from the eyeball.
func logHTTPRequest(logger *zerolog.Logger, r *http.Request) {
	logger.Debug().
		Str("host", r.Host).
		Str("path", r.URL.Path).
		Interface("headers", r.Header).
		Int64("content-length", r.ContentLength).
		Msgf("%s %s %s", r.Method, r.URL, r.Proto)
}

// logOriginHTTPResponse logs a Debug message of the origin response.
func logOriginHTTPResponse(logger *zerolog.Logger, resp *http.Response) {
	responseByCode.WithLabelValues(strconv.Itoa(resp.StatusCode)).Inc()
	logger.Debug().
		Int64("content-length", resp.ContentLength).
		Msgf("%s", resp.Status)
}

// logRequestError logs an error for the proxied request.
// Benign remote stream cancellations (QUIC NO_ERROR) are logged at Debug and
// do not increment the request-error metric — see connection.IsBenignRemoteStreamCancel.
func logRequestError(logger *zerolog.Logger, err error) {
	if connection.IsBenignRemoteStreamCancel(err) {
		logger.Debug().Err(err).Msg("request stream canceled by remote with NO_ERROR")
		return
	}
	requestErrors.Inc()
	logger.Error().Err(err).Send()
}
