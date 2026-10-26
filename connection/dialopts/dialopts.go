package dialopts

// DialOpts holds the configuration for dialing a QUIC connection.
type DialOpts struct {
	// SkipPortReuse skips UDP port reuse. This is useful for probe connections
	// that should use a random ephemeral port to avoid interfering with the
	// main connection flow.
	SkipPortReuse bool
}
