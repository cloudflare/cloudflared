package binary

type CompressionMethodByte byte

const (
	NONE CompressionMethodByte = 0x02
	LZ4                        = 0x82
	ZSTD                       = 0x90
)

const (
	// ChecksumSize is 128bits for cityhash102 checksum
	ChecksumSize = 16
	// CompressHeader magic + compressed_size + uncompressed_size
	CompressHeaderSize = 1 + 4 + 4

	// HeaderSize
	HeaderSize = ChecksumSize + CompressHeaderSize
	// BlockMaxSize 1MB
	BlockMaxSize = 1 << 10
)
