package token

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

const (
	baseStoreURL  = "https://login.cloudflareaccess.org/"
	clientTimeout = time.Second * 60
)

// RunTransfer does the transfer "dance" with the end result downloading the supported resource.
// The expanded description is run is encapsulation of shared business logic needed
// to request a resource (token/cert/etc) from the transfer service (loginhelper).
// The "dance" we refer to is building a HTTP request, opening that in a browser waiting for
// the user to complete an action, while it long polls in the background waiting for an
// action to be completed to download the resource.
func RunTransfer(transferURL *url.URL, resourceName, key, value string, shouldEncrypt bool, useHostOnly bool, log *zerolog.Logger) ([]byte, error) {
	encrypterClient, err := NewEncrypter("cloudflared_priv.pem", "cloudflared_pub.pem")
	if err != nil {
		return nil, err
	}
	requestURL, err := buildRequestURL(transferURL, key, value+encrypterClient.PublicKey(), shouldEncrypt, useHostOnly)
	if err != nil {
		return nil, err
	}

	// See AUTH-1423 for why we use stderr (the way git wraps ssh)
	err = OpenBrowser(requestURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Please open the following URL and log in with your Cloudflare account:\n\n%s\n\nLeave cloudflared running to download the %s automatically.\n", requestURL, resourceName)
	} else {
		fmt.Fprintf(os.Stderr, "A browser window should have opened at the following URL:\n\n%s\n\nIf the browser failed to open, please visit the URL above directly in your browser.\n", requestURL)
	}

	var resourceData []byte

	if shouldEncrypt {
		buf, key, err := transferRequest(baseStoreURL+"transfer/"+encrypterClient.PublicKey(), log)
		if err != nil {
			return nil, err
		}

		decodedBuf, err := base64.StdEncoding.DecodeString(string(buf))
		if err != nil {
			return nil, err
		}
		decrypted, err := encrypterClient.Decrypt(decodedBuf, key)
		if err != nil {
			return nil, err
		}

		resourceData = decrypted
	} else {
		buf, _, err := transferRequest(baseStoreURL+encrypterClient.PublicKey(), log)
		if err != nil {
			return nil, err
		}
		resourceData = buf
	}

	return resourceData, nil

}

// BuildRequestURL creates a request suitable for a resource transfer.
// it will return a constructed url based off the base url and query key/value provided.
// cli will build a url for cli transfer request.
func buildRequestURL(baseURL *url.URL, key, value string, cli, useHostOnly bool) (string, error) {
	q := baseURL.Query()
	q.Set(key, value)
	baseURL.RawQuery = q.Encode()
	if useHostOnly {
		baseURL.Path = ""
	}
	// TODO: pass arg for tunnel login
	if !cli {
		return baseURL.String(), nil
	}
	q.Set("redirect_url", baseURL.String()) // we add the token as a query param on both the redirect_url and the main url
	q.Set("send_org_token", "true")         // indicates that the cli endpoint should return both the org and app token
	q.Set("edge_token_transfer", "true")    // use new LoginHelper service built on workers
	baseURL.RawQuery = q.Encode()           // and this actual baseURL.
	baseURL.Path = "cdn-cgi/access/cli"
	return baseURL.String(), nil
}

// transferRequest downloads the requested resource from the request URL
func transferRequest(requestURL string, log *zerolog.Logger) ([]byte, string, error) {
	client := &http.Client{Timeout: clientTimeout}
	const pollAttempts = 10
	// we do "long polling" on the endpoint to get the resource.
	for i := 0; i < pollAttempts; i++ {
		buf, key, err := poll(client, requestURL, log)
		if err != nil {
			return nil, "", err
		} else if len(buf) > 0 {
			return buf, key, nil
		}
	}
	return nil, "", errors.New("Failed to fetch resource")
}

// poll the endpoint for the request resource, waiting for the user interaction
func poll(client *http.Client, requestURL string, log *zerolog.Logger) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	// ignore everything other than server errors as the resource
	// may not exist until the user does the interaction
	if resp.StatusCode >= 500 {
		return nil, "", fmt.Errorf("error on request %d", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		log.Info().Msg("Waiting for login...")
		return nil, "", nil
	}

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, resp.Body); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), resp.Header.Get("service-public-key"), nil
}
