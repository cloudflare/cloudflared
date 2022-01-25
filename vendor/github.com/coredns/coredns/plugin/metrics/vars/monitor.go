package vars

import (
	"github.com/miekg/dns"
)

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
	dns.TypeHTTPS:  {},
	// Meta Qtypes
	dns.TypeIXFR: {},
	dns.TypeAXFR: {},
	dns.TypeANY:  {},
}

// qTypeString returns the RR type based on monitorType. It returns the text representation
// of those types. RR types not in that list will have "other" returned.
func qTypeString(qtype uint16) string {
	if _, known := monitorType[qtype]; known {
		return dns.Type(qtype).String()
	}
	return "other"
}
