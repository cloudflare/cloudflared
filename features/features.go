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
