package features

const (
	FeatureSerializedHeaders = "serialized_headers"
	FeatureQuickReconnects   = "quick_reconnects"
	FeatureAllowRemoteConfig = "allow_remote_config"
	FeatureDatagramV2        = "support_datagram_v2"
	FeaturePostQuantum       = "postquantum"
	FeatureQUICSupportEOF    = "support_quic_eof"
	FeatureManagementLogs    = "management_logs"
	FeatureDatagramV3        = "support_datagram_v3"
)

var defaultFeatures = []string{
	FeatureAllowRemoteConfig,
	FeatureSerializedHeaders,
	FeatureDatagramV2,
	FeatureQUICSupportEOF,
	FeatureManagementLogs,
}

// Features set by user provided flags
type staticFeatures struct {
	PostQuantumMode *PostQuantumMode
}

type PostQuantumMode uint8

const (
	// Prefer post quantum, but fallback if connection cannot be established
	PostQuantumPrefer PostQuantumMode = iota
	// If the user passes the --post-quantum flag, we override
	// CurvePreferences to only support hybrid post-quantum key agreements.
	PostQuantumStrict
)

type DatagramVersion string

const (
	// DatagramV2 is the currently supported datagram protocol for UDP and ICMP packets
	DatagramV2 DatagramVersion = FeatureDatagramV2
	// DatagramV3 is a new datagram protocol for UDP and ICMP packets. It is not backwards compatible with datagram v2.
	DatagramV3 DatagramVersion = FeatureDatagramV3
)

// Remove any duplicates from the slice
func Dedup(slice []string) []string {
	// Convert the slice into a set
	set := make(map[string]bool, 0)
	for _, str := range slice {
		set[str] = true
	}

	// Convert the set back into a slice
	keys := make([]string, len(set))
	i := 0
	for str := range set {
		keys[i] = str
		i++
	}
	return keys
}
