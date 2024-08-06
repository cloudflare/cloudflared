package management

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func TestValidateAccessTokenQueryMiddleware(t *testing.T) {
	r := chi.NewRouter()
	r.Use(ValidateAccessTokenQueryMiddleware)
	r.Get("/valid", func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(accessClaimsCtxKey).(*managementTokenClaims)
		assert.True(t, ok)
		assert.True(t, claims.verify())
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/invalid", func(w http.ResponseWriter, r *http.Request) {
		_, ok := r.Context().Value(accessClaimsCtxKey).(*managementTokenClaims)
		assert.False(t, ok)
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(r)
	defer ts.Close()

	// valid: with access_token query param
	path := "/valid?access_token=" + validToken
	resp, _ := testRequest(t, ts, "GET", path, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// invalid: unset token
	path = "/invalid"
	resp, err := testRequest(t, ts, "GET", path, nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.NotNil(t, err)
	assert.Equal(t, errMissingAccessToken, err.Errors[0])

	// invalid: invalid token
	path = "/invalid?access_token=eyJ"
	resp, err = testRequest(t, ts, "GET", path, nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.NotNil(t, err)
	assert.Equal(t, errMissingAccessToken, err.Errors[0])
}

func testRequest(t *testing.T, ts *httptest.Server, method, path string, body io.Reader) (*http.Response, *managementErrorResponse) {
	req, err := http.NewRequest(method, ts.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var claims managementErrorResponse
	err = json.NewDecoder(resp.Body).Decode(&claims)
	if err != nil {
		return resp, nil
	}
	defer resp.Body.Close()

	return resp, &claims
}
