package transfer

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/encrypter"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/shell"
	"github.com/cloudflare/cloudflared/log"
)

const (
	baseStoreURL  = "https://login.cloudflarewarp.com/"
	clientTimeout = time.Second * 60
)

var logger = log.CreateLogger()

// Run does the transfer "dance" with the end result downloading the supported resource.
// The expanded description is run is encapsulation of shared business logic needed
// to request a resource (token/cert/etc) from the transfer service (loginhelper).
// The "dance" we refer to is building a HTTP request, opening that in a browser waiting for
// the user to complete an action, while it long polls in the background waiting for an
// action to be completed to download the resource.
func Run(transferURL *url.URL, resourceName, key, value, path string, shouldEncrypt bool) ([]byte, error) {
	encrypterClient, err := encrypter.New("cloudflared_priv.pem", "cloudflared_pub.pem")
	if err != nil {
		return nil, err
	}
	requestURL, err := buildRequestURL(transferURL, key, value+encrypterClient.PublicKey(), shouldEncrypt)
	if err != nil {
		return nil, err
	}

	err = shell.OpenBrowser(requestURL)
	if err != nil {
		fmt.Fprintf(os.Stdout, "Please open the following URL and log in with your Cloudflare account:\n\n%s\n\nLeave cloudflared running to download the %s automatically.\n", requestURL, resourceName)
	} else {
		fmt.Fprintf(os.Stdout, "A browser window should have opened at the following URL:\n\n%s\n\nIf the browser failed to open, open it yourself and visit the URL above.\n", requestURL)
	}

	var resourceData []byte

	if shouldEncrypt {
		buf, key, err := transferRequest(baseStoreURL + filepath.Join("transfer", encrypterClient.PublicKey()))
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
		buf, _, err := transferRequest(baseStoreURL + filepath.Join(encrypterClient.PublicKey()))
		if err != nil {
			return nil, err
		}
		resourceData = buf
	}

	if err := ioutil.WriteFile(path, resourceData, 0600); err != nil {
		return nil, err
	}

	return resourceData, nil
}

// BuildRequestURL creates a request suitable for a resource transfer.
// it will return a constructed url based off the base url and query key/value provided.
// cli will build a url for cli transfer request.
func buildRequestURL(baseURL *url.URL, key, value string, cli bool) (string, error) {
	q := baseURL.Query()
	q.Set(key, value)
	baseURL.RawQuery = q.Encode()
	if !cli {
		return baseURL.String(), nil
	}

	q.Set("redirect_url", baseURL.String()) // we add the token as a query param on both the redirect_url
	baseURL.RawQuery = q.Encode()           // and this actual baseURL.
	baseURL.Path = "cdn-cgi/access/cli"
	return baseURL.String(), nil
}

// transferRequest downloads the requested resource from the request URL
func transferRequest(requestURL string) ([]byte, string, error) {
	client := &http.Client{Timeout: clientTimeout}
	const pollAttempts = 10
	// we do "long polling" on the endpoint to get the resource.
	for i := 0; i < pollAttempts; i++ {
		buf, key, err := poll(client, requestURL)
		if err != nil {
			return nil, "", err
		} else if len(buf) > 0 {
			if err := putSuccess(client, requestURL); err != nil {
				logger.WithError(err).Error("Failed to update resource success")
			}
			return buf, key, nil
		}
	}
	return nil, "", errors.New("Failed to fetch resource")
}

// poll the endpoint for the request resource, waiting for the user interaction
func poll(client *http.Client, requestURL string) ([]byte, string, error) {
	resp, err := client.Get(requestURL)
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
		logger.Info("Waiting for login...")
		return nil, "", nil
	}

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, resp.Body); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), resp.Header.Get("service-public-key"), nil
}

// putSuccess tells the server we successfully downloaded the resource
func putSuccess(client *http.Client, requestURL string) error {
	req, err := http.NewRequest("PUT", requestURL+"/ok", nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP Response Status Code: %d", resp.StatusCode)
	}
	return nil
}
