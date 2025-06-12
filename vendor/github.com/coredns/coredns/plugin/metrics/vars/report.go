package vars

import (
	"time"

	"github.com/coredns/coredns/request"
)

// ReportOptions is a struct that contains available options for the Report function.
type ReportOptions struct {
	OriginalReqSize int
}

// ReportOption defines a function that modifies ReportOptions
type ReportOption func(*ReportOptions)

// WithOriginalReqSize returns an option to set the original request size
func WithOriginalReqSize(size int) ReportOption {
	return func(opts *ReportOptions) {
		opts.OriginalReqSize = size
	}
}

// Report reports the metrics data associated with request. This function is exported because it is also
// called from core/dnsserver to report requests hitting the server that should not be handled and are thus
// not sent down the plugin chain.
func Report(server string, req request.Request, zone, view, rcode, plugin string,
	size int, start time.Time, opts ...ReportOption) {
	options := ReportOptions{
		OriginalReqSize: 0,
	}

	for _, opt := range opts {
		opt(&options)
	}

	// Proto and Family.
	net := req.Proto()
	fam := "1"
	if req.Family() == 2 {
		fam = "2"
	}

	if req.Do() {
		RequestDo.WithLabelValues(server, zone, view).Inc()
	}

	qType := qTypeString(req.QType())
	RequestCount.WithLabelValues(server, zone, view, net, fam, qType).Inc()

	RequestDuration.WithLabelValues(server, zone, view).Observe(time.Since(start).Seconds())

	ResponseSize.WithLabelValues(server, zone, view, net).Observe(float64(size))

	reqSize := req.Len()
	if options.OriginalReqSize > 0 {
		reqSize = options.OriginalReqSize
	}

	RequestSize.WithLabelValues(server, zone, view, net).Observe(float64(reqSize))

	ResponseRcode.WithLabelValues(server, zone, view, rcode, plugin).Inc()
}
