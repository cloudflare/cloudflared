package metrics

import (
	"runtime"

	"github.com/coredns/coredns/plugin/pkg/dnstest"

	"github.com/miekg/dns"
)

// Recorder is a dnstest.Recorder specific to the metrics plugin.
type Recorder struct {
	*dnstest.Recorder
	// CallerN holds the string return value of the call to runtime.Caller(N+1)
	Caller [3]string
}

// NewRecorder makes and returns a new Recorder.
func NewRecorder(w dns.ResponseWriter) *Recorder { return &Recorder{Recorder: dnstest.NewRecorder(w)} }

// WriteMsg records the status code and calls the
// underlying ResponseWriter's WriteMsg method.
func (r *Recorder) WriteMsg(res *dns.Msg) error {
	_, r.Caller[0], _, _ = runtime.Caller(1)
	_, r.Caller[1], _, _ = runtime.Caller(2)
	_, r.Caller[2], _, _ = runtime.Caller(3)
	return r.Recorder.WriteMsg(res)
}
