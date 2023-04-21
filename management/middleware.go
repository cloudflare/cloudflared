package management

import (
	"context"
	"fmt"
	"net/http"
)

type ctxKey int

const (
	accessClaimsCtxKey ctxKey = iota
)

const (
	connectorIDQuery = "connector_id"
	accessTokenQuery = "access_token"
)

var (
	errMissingAccessToken = managementError{Code: 1001, Message: "missing access_token query parameter"}
)

// HTTP middleware setting the parsed access_token claims in the request context
func ValidateAccessTokenQueryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate access token
		accessToken := r.URL.Query().Get("access_token")
		if accessToken == "" {
			writeHTTPErrorResponse(w, errMissingAccessToken)
			return
		}
		token, err := parseToken(accessToken)
		if err != nil {
			writeHTTPErrorResponse(w, errMissingAccessToken)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), accessClaimsCtxKey, token))
		next.ServeHTTP(w, r)
	})
}

// Middleware validation error struct for returning to the eyeball
type managementError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (m *managementError) Error() string {
	return m.Message
}

// Middleware validation error HTTP response JSON for returning to the eyeball
type managementErrorResponse struct {
	Success bool              `json:"success,omitempty"`
	Errors  []managementError `json:"errors,omitempty"`
}

// writeErrorResponse will respond to the eyeball with basic HTTP JSON payloads with validation failure information
func writeHTTPErrorResponse(w http.ResponseWriter, errResp managementError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	err := json.NewEncoder(w).Encode(managementErrorResponse{
		Success: false,
		Errors:  []managementError{errResp},
	})
	// we have already written the header, so write a basic error response if unable to encode the error
	if err != nil {
		// fallback to text message
		http.Error(w, fmt.Sprintf(
			"%d %s",
			http.StatusBadRequest,
			http.StatusText(http.StatusBadRequest),
		), http.StatusBadRequest)
	}
}
