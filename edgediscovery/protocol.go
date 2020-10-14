package edgediscovery

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

const (
	protocolRecord = "protocol.argotunnel.com"
)

var (
	errNoProtocolRecord = fmt.Errorf("No TXT record found for %s to determine connection protocol", protocolRecord)
)

func HTTP2Percentage() (int32, error) {
	records, err := net.LookupTXT(protocolRecord)
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, errNoProtocolRecord
	}
	return parseHTTP2Precentage(records[0])
}

// The record looks like http2=percentage
func parseHTTP2Precentage(record string) (int32, error) {
	const key = "http2"
	slices := strings.Split(record, "=")
	if len(slices) != 2 {
		return 0, fmt.Errorf("Malformed TXT record %s, expect http2=percentage", record)
	}
	if slices[0] != key {
		return 0, fmt.Errorf("Incorrect key %s, expect %s", slices[0], key)
	}
	percentage, err := strconv.ParseInt(slices[1], 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(percentage), nil

}
