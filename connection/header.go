package connection

import (
	"fmt"
	"net/http"

	"github.com/cloudflare/cloudflared/h2mux"
)

const (
	responseMetaHeaderField   = "cf-cloudflared-response-meta"
	responseSourceCloudflared = "cloudflared"
	responseSourceOrigin      = "origin"
)

var (
	canonicalResponseUserHeadersField = http.CanonicalHeaderKey(h2mux.ResponseUserHeadersField)
	canonicalResponseMetaHeaderField  = http.CanonicalHeaderKey(responseMetaHeaderField)
	responseMetaHeaderCfd             = mustInitRespMetaHeader(responseSourceCloudflared)
	responseMetaHeaderOrigin          = mustInitRespMetaHeader(responseSourceOrigin)
)

type responseMetaHeader struct {
	Source string `json:"src"`
}

func mustInitRespMetaHeader(src string) string {
	header, err := json.Marshal(responseMetaHeader{Source: src})
	if err != nil {
		panic(fmt.Sprintf("Failed to serialize response meta header = %s, err: %v", responseSourceCloudflared, err))
	}
	return string(header)
}
