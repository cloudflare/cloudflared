package supervisor

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"sync"
)

// When experimental post-quantum tunnels are enabled, and we're hitting an
// issue creating the tunnel, we'll report the first error
// to https://pqtunnels.cloudflareresearch.com.

var (
	PQKexes = [...]tls.CurveID{
		tls.CurveID(0xfe30), // X25519Kyber512Draft00
		tls.CurveID(0xfe31), // X25519Kyber768Draft00
	}
	PQKexNames map[tls.CurveID]string = map[tls.CurveID]string{
		tls.CurveID(0xfe30): "X25519Kyber512Draft00",
		tls.CurveID(0xfe31): "X25519Kyber768Draft00",
	}

	pqtMux       sync.Mutex // protects pqtSubmitted and pqtWaitForMessage
	pqtSubmitted bool       // whether an error has already been submitted

	// Number of errors to ignore before printing elaborate instructions.
	pqtWaitForMessage int
)

func handlePQTunnelError(rep error, config *TunnelConfig) {
	needToMessage := false

	pqtMux.Lock()
	needToSubmit := !pqtSubmitted
	if needToSubmit {
		pqtSubmitted = true
	}
	pqtWaitForMessage--
	if pqtWaitForMessage < 0 {
		pqtWaitForMessage = 5
		needToMessage = true
	}
	pqtMux.Unlock()

	if needToMessage {
		config.Log.Info().Msgf(
			"\n\n" +
				"===================================================================================\n" +
				"You are hitting an error while using the experimental post-quantum tunnels feature.\n" +
				"\n" +
				"Please check:\n" +
				"\n" +
				"   https://pqtunnels.cloudflareresearch.com\n" +
				"\n" +
				"for known problems.\n" +
				"===================================================================================\n\n",
		)
	}

	if needToSubmit {
		go submitPQTunnelError(rep, config)
	}
}

func submitPQTunnelError(rep error, config *TunnelConfig) {
	body, err := json.Marshal(struct {
		Group   int    `json:"g"`
		Message string `json:"m"`
		Version string `json:"v"`
	}{
		Group:   int(PQKexes[config.PQKexIdx]),
		Message: rep.Error(),
		Version: config.ReportedVersion,
	})
	if err != nil {
		config.Log.Err(err).Msg("Failed to create error report")
		return
	}

	resp, err := http.Post(
		"https://pqtunnels.cloudflareresearch.com",
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		config.Log.Err(err).Msg(
			"Failed to submit post-quantum tunnel error report",
		)
		return
	}
	if resp.StatusCode != 200 {
		config.Log.Error().Msgf(
			"Failed to submit post-quantum tunnel error report: status %d",
			resp.StatusCode,
		)
	}
	resp.Body.Close()
}
