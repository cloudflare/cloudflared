package vars

import (
	"time"

	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

// Report reports the metrics data associated with request. This function is exported because it is also
// called from core/dnsserver to report requests hitting the server that should not be handled and are thus
// not sent down the plugin chain.
func Report(server string, req request.Request, zone, rcode string, size int, start time.Time) {
	// Proto and Family.
	net := req.Proto()
	fam := "1"
	if req.Family() == 2 {
		fam = "2"
	}

	typ := req.QType()

	if req.Do() {
		RequestDo.WithLabelValues(server, zone).Inc()
	}

	if _, known := monitorType[typ]; known {
		RequestCount.WithLabelValues(server, zone, net, fam, dns.Type(typ).String()).Inc()
		RequestDuration.WithLabelValues(server, zone, dns.Type(typ).String()).Observe(time.Since(start).Seconds())
	} else {
		RequestCount.WithLabelValues(server, zone, net, fam, other).Inc()
		RequestDuration.WithLabelValues(server, zone, other).Observe(time.Since(start).Seconds())
	}

	ResponseSize.WithLabelValues(server, zone, net).Observe(float64(size))
	RequestSize.WithLabelValues(server, zone, net).Observe(float64(req.Len()))

	ResponseRcode.WithLabelValues(server, zone, rcode).Inc()
}

var monitorType = map[uint16]struct{}{
	dns.TypeAAAA:   {},
	dns.TypeA:      {},
	dns.TypeCNAME:  {},
	dns.TypeDNSKEY: {},
	dns.TypeDS:     {},
	dns.TypeMX:     {},
	dns.TypeNSEC3:  {},
	dns.TypeNSEC:   {},
	dns.TypeNS:     {},
	dns.TypePTR:    {},
	dns.TypeRRSIG:  {},
	dns.TypeSOA:    {},
	dns.TypeSRV:    {},
	dns.TypeTXT:    {},
	// Meta Qtypes
	dns.TypeIXFR: {},
	dns.TypeAXFR: {},
	dns.TypeANY:  {},
}

const other = "other"
