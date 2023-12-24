package token

import (
	"net/http"
	"net/url"
	"testing"
)

func TestHandleRedirects_AttachOrgToken(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com/cdn-cgi/access/login", nil)
	via := []*http.Request{}
	orgToken := "orgTokenValue"

	handleRedirects(req, via, orgToken)

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
