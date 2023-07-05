package access

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildRequestHeaders(t *testing.T) {
	headers := make(http.Header)
	headers.Add("client", "value")
	headers.Add("secret", "safe-value")

	values := buildRequestHeaders([]string{"client: value", "secret: safe-value", "trash", "cf-trace-id: 000:000:0:1:asd"})
	assert.Equal(t, headers.Get("client"), values.Get("client"))
	assert.Equal(t, headers.Get("secret"), values.Get("secret"))
	assert.Equal(t, headers.Get("cf-trace-id"), values.Get("000:000:0:1:asd"))
}
