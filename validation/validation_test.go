package validation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateHostname(t *testing.T) {
	var inputHostname string
	hostname, err := ValidateHostname(inputHostname)
	assert.Equal(t, err, nil)
	assert.Empty(t, hostname)

	inputHostname = "hello.example.com"
	hostname, err = ValidateHostname(inputHostname)
	assert.Nil(t, err)
	assert.Equal(t, "hello.example.com", hostname)

	inputHostname = "http://hello.example.com"
	hostname, err = ValidateHostname(inputHostname)
	assert.Nil(t, err)
	assert.Equal(t, "hello.example.com", hostname)

	inputHostname = "bücher.example.com"
	hostname, err = ValidateHostname(inputHostname)
	assert.Nil(t, err)
	assert.Equal(t, "xn--bcher-kva.example.com", hostname)

	inputHostname = "http://bücher.example.com"
	hostname, err = ValidateHostname(inputHostname)
	assert.Nil(t, err)
	assert.Equal(t, "xn--bcher-kva.example.com", hostname)

	inputHostname = "http%3A%2F%2Fhello.example.com"
	hostname, err = ValidateHostname(inputHostname)
	assert.Nil(t, err)
	assert.Equal(t, "hello.example.com", hostname)

}

func TestValidateUrl(t *testing.T) {
	validUrl, err := ValidateUrl("")
	assert.Equal(t, fmt.Errorf("Url should not be empty"), err)
	assert.Empty(t, validUrl)

	validUrl, err = ValidateUrl("https://localhost:8080")
	assert.Nil(t, err)
	assert.Equal(t, "https://localhost:8080", validUrl)

	validUrl, err = ValidateUrl("localhost:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://localhost:8080", validUrl)

	validUrl, err = ValidateUrl("http://localhost")
	assert.Nil(t, err)
	assert.Equal(t, "http://localhost", validUrl)

	validUrl, err = ValidateUrl("http://127.0.0.1:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://127.0.0.1:8080", validUrl)

	validUrl, err = ValidateUrl("127.0.0.1:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://127.0.0.1:8080", validUrl)

	validUrl, err = ValidateUrl("127.0.0.1")
	assert.Nil(t, err)
	assert.Equal(t, "http://127.0.0.1", validUrl)

	validUrl, err = ValidateUrl("https://127.0.0.1:8080")
	assert.Nil(t, err)
	assert.Equal(t, "https://127.0.0.1:8080", validUrl)

	validUrl, err = ValidateUrl("[::1]:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://[::1]:8080", validUrl)

	validUrl, err = ValidateUrl("http://[::1]")
	assert.Nil(t, err)
	assert.Equal(t, "http://[::1]", validUrl)

	validUrl, err = ValidateUrl("http://[::1]:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://[::1]:8080", validUrl)

	validUrl, err = ValidateUrl("[::1]")
	assert.Nil(t, err)
	assert.Equal(t, "http://[::1]", validUrl)

	validUrl, err = ValidateUrl("https://example.com")
	assert.Nil(t, err)
	assert.Equal(t, "https://example.com", validUrl)

	validUrl, err = ValidateUrl("example.com")
	assert.Nil(t, err)
	assert.Equal(t, "http://example.com", validUrl)

	validUrl, err = ValidateUrl("http://hello.example.com")
	assert.Nil(t, err)
	assert.Equal(t, "http://hello.example.com", validUrl)

	validUrl, err = ValidateUrl("hello.example.com")
	assert.Nil(t, err)
	assert.Equal(t, "http://hello.example.com", validUrl)

	validUrl, err = ValidateUrl("hello.example.com:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://hello.example.com:8080", validUrl)

	validUrl, err = ValidateUrl("https://hello.example.com:8080")
	assert.Nil(t, err)
	assert.Equal(t, "https://hello.example.com:8080", validUrl)

	validUrl, err = ValidateUrl("https://bücher.example.com")
	assert.Nil(t, err)
	assert.Equal(t, "https://xn--bcher-kva.example.com", validUrl)

	validUrl, err = ValidateUrl("bücher.example.com")
	assert.Nil(t, err)
	assert.Equal(t, "http://xn--bcher-kva.example.com", validUrl)

	validUrl, err = ValidateUrl("https%3A%2F%2Fhello.example.com")
	assert.Nil(t, err)
	assert.Equal(t, "https://hello.example.com", validUrl)

	validUrl, err = ValidateUrl("ftp://alex:12345@hello.example.com:8080/robot.txt")
	assert.Equal(t, "Currently Argo Tunnel does not support ftp protocol.", err.Error())
	assert.Empty(t, validUrl)

	validUrl, err = ValidateUrl("https://alex:12345@hello.example.com:8080")
	assert.Nil(t, err)
	assert.Equal(t, "https://hello.example.com:8080", validUrl)

}
