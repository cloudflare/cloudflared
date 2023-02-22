package vars

import (
	"time"

	"github.com/coredns/coredns/request"
)

// Report reports the metrics data associated with request. This function is exported because it is also
// called from core/dnsserver to report requests hitting the server that should not be handled and are thus
// not sent down the plugin chain.
func Report(server string, req request.Request, zone, view, rcode, plugin string, size int, start time.Time) {
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
	RequestSize.WithLabelValues(server, zone, view, net).Observe(float64(req.Len()))

	ResponseRcode.WithLabelValues(server, zone, view, rcode, plugin).Inc()
}
