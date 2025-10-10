package features

import "slices"

const (
	FeatureSerializedHeaders = "serialized_headers"
	FeatureQuickReconnects   = "quick_reconnects"
	FeatureAllowRemoteConfig = "allow_remote_config"
	FeatureDatagramV2        = "support_datagram_v2"
	FeaturePostQuantum       = "postquantum"
	FeatureQUICSupportEOF    = "support_quic_eof"
	FeatureManagementLogs    = "management_logs"
	FeatureDatagramV3_2      = "support_datagram_v3_2"

	DeprecatedFeatureDatagramV3   = "support_datagram_v3"   // Deprecated: TUN-9291
	DeprecatedFeatureDatagramV3_1 = "support_datagram_v3_1" // Deprecated: TUN-9883
)

var defaultFeatures = []string{
	FeatureAllowRemoteConfig,
	FeatureSerializedHeaders,
	FeatureDatagramV2,
	FeatureQUICSupportEOF,
	FeatureManagementLogs,
}

// List of features that are no longer in-use.
var deprecatedFeatures = []string{
	DeprecatedFeatureDatagramV3,
	DeprecatedFeatureDatagramV3_1,
}

// Features set by user provided flags
type staticFeatures struct {
	PostQuantumMode *PostQuantumMode
}

type FeatureSnapshot struct {
	PostQuantum     PostQuantumMode
	DatagramVersion DatagramVersion

	// We provide the list of features since we need it to send in the ConnectionOptions during connection
	// registrations.
	FeaturesList []string
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
	DatagramV3 DatagramVersion = FeatureDatagramV3_2
)

// Remove any duplicate features from the list and remove deprecated features
func dedupAndRemoveFeatures(features []string) []string {
	// Convert the slice into a set
	set := map[string]bool{}
	for _, feature := range features {
		// Remove deprecated features from the provided list
		if slices.Contains(deprecatedFeatures, feature) {
			continue
		}
		set[feature] = true
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
