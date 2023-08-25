package features

const (
	FeatureSerializedHeaders = "serialized_headers"
	FeatureQuickReconnects   = "quick_reconnects"
	FeatureAllowRemoteConfig = "allow_remote_config"
	FeatureDatagramV2        = "support_datagram_v2"
	FeaturePostQuantum       = "postquantum"
	FeatureQUICSupportEOF    = "support_quic_eof"
	FeatureManagementLogs    = "management_logs"
)

var (
	DefaultFeatures = []string{
		FeatureAllowRemoteConfig,
		FeatureSerializedHeaders,
		FeatureDatagramV2,
		FeatureQUICSupportEOF,
		FeatureManagementLogs,
	}
)

func Contains(feature string) bool {
	for _, f := range DefaultFeatures {
		if f == feature {
			return true
		}
	}
	return false
}

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
