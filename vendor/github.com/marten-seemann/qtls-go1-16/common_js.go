// +build js

package qtls

var (
	hasGCMAsmAMD64 = false
	hasGCMAsmARM64 = false
	// Keep in sync with crypto/aes/cipher_s390x.go.
	hasGCMAsmS390X = false

	hasAESGCMHardwareSupport = false
)
