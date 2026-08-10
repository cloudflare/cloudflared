package token

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHandleRedirects_AttachOrgToken(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com/cdn-cgi/access/login", nil)
	via := []*http.Request{}
	orgToken := "orgTokenValue"

	_ = handleRedirects(req, via, orgToken)

	// Check if the orgToken cookie is attached
	cookies := req.Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == tokenCookie && cookie.Value == orgToken {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("OrgToken cookie not attached to the request.")
	}
}

func TestHandleRedirects_AttachAppSessionCookie(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com/cdn-cgi/access/authorized", nil)
	via := []*http.Request{
		{
			URL: &url.URL{Path: "/cdn-cgi/access/login"},
			Response: &http.Response{
				Header: http.Header{"Set-Cookie": {"CF_AppSession=appSessionValue"}},
			},
		},
	}
	orgToken := "orgTokenValue"

	err := handleRedirects(req, via, orgToken)

	// Check if the appSessionCookie is attached to the request
	cookies := req.Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == appSessionCookie && cookie.Value == "appSessionValue" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("AppSessionCookie not attached to the request.")
	}

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestHandleRedirects_StopAtAuthorizedEndpoint(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com/cdn-cgi/access/authorized", nil)
	via := []*http.Request{
		{
			URL: &url.URL{Path: "other"},
		},
		{
			URL: &url.URL{Path: AccessAuthorizedWorkerPath},
		},
	}
	orgToken := "orgTokenValue"

	err := handleRedirects(req, via, orgToken)

	// Check if ErrUseLastResponse is returned
	if err != http.ErrUseLastResponse {
		t.Errorf("Expected ErrUseLastResponse, got %v", err)
	}
}

func TestJwtPayloadUnmarshal_AudAsString(t *testing.T) {
	jwt := `{"aud":"7afbdaf987054f889b3bdd0d29ebfcd2"}`
	var payload jwtPayload
	if err := json.Unmarshal([]byte(jwt), &payload); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(payload.Aud) != 1 || payload.Aud[0] != "7afbdaf987054f889b3bdd0d29ebfcd2" {
		t.Errorf("Expected aud to be 7afbdaf987054f889b3bdd0d29ebfcd2, got %v", payload.Aud)
	}
}

func TestJwtPayloadUnmarshal_AudAsSlice(t *testing.T) {
	jwt := `{"aud":["7afbdaf987054f889b3bdd0d29ebfcd2", "f835c0016f894768976c01e076844efe"]}`
	var payload jwtPayload
	if err := json.Unmarshal([]byte(jwt), &payload); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(payload.Aud) != 2 || payload.Aud[0] != "7afbdaf987054f889b3bdd0d29ebfcd2" || payload.Aud[1] != "f835c0016f894768976c01e076844efe" {
		t.Errorf("Expected aud to be [7afbdaf987054f889b3bdd0d29ebfcd2, f835c0016f894768976c01e076844efe], got %v", payload.Aud)
	}
}

func TestJwtPayloadUnmarshal_FailsWhenAudIsInt(t *testing.T) {
	jwt := `{"aud":123}`
	var payload jwtPayload
	err := json.Unmarshal([]byte(jwt), &payload)
	wantErr := "aud field is not a string or an array of strings"
	if err.Error() != wantErr {
		t.Errorf("Expected %v, got %v", wantErr, err)
	}
}

func TestJwtPayloadUnmarshal_FailsWhenAudIsArrayOfInts(t *testing.T) {
	jwt := `{"aud": [999, 123] }`
	var payload jwtPayload
	err := json.Unmarshal([]byte(jwt), &payload)
	wantErr := "aud array contains non-string elements"
	if err.Error() != wantErr {
		t.Errorf("Expected %v, got %v", wantErr, err)
	}
}

func TestJwtPayloadUnmarshal_FailsWhenAudIsOmitted(t *testing.T) {
	jwt := `{}`
	var payload jwtPayload
	err := json.Unmarshal([]byte(jwt), &payload)
	wantErr := "aud field is not a string or an array of strings"
	if err.Error() != wantErr {
		t.Errorf("Expected %v, got %v", wantErr, err)
	}
}

func TestIsCloudflareAccessHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"team.cloudflareaccess.com", true},
		{"team.cloudflareaccess.com:8443", true},
		{"TEAM.cloudflareaccess.com", true},
		{"sub.team.cloudflareaccess.com", true},
		{"cloudflareaccess.com", false},
		{".cloudflareaccess.com", false},
		{"attacker.com", false},
		{"127.0.0.1", false},
		{"127.0.0.1:8080", false},
		{"cloudflareaccess.com.attacker.com", false},
		{"team-cloudflareaccess.com", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isCloudflareAccessHost(c.host); got != c.want {
			t.Errorf("isCloudflareAccessHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestGetAppInfo_RejectsRedirectFromNonCloudflareHost(t *testing.T) {
	// Stand up a server that mimics the attacker shape: a HEAD to /
	// redirects to /cdn-cgi/access/login?kid=AUD on the same host, which
	// returns 200 with CF-Access-Domain set. Without host validation, the
	// (AppDomain, AppAUD) fields would come from this attacker-controlled
	// response. With host validation, GetAppInfo must refuse it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/cdn-cgi/access/login?kid=AUD123", http.StatusFound)
			return
		}
		if strings.Contains(r.URL.Path, AccessLoginWorkerPath) {
			w.Header().Set(appDomainHeader, "victim-app.example.com")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	reqURL, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	appInfo, err := GetAppInfo(reqURL)
	if err == nil {
		t.Fatalf("GetAppInfo accepted response from non-Cloudflare host; got appInfo=%+v", appInfo)
	}
	if !strings.Contains(err.Error(), "non-Cloudflare host") {
		t.Fatalf("expected non-Cloudflare host error, got: %v", err)
	}
}
