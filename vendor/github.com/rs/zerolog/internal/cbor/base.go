package cbor

type Encoder struct{}

// AppendKey adds a key (string) to the binary encoded log message
func (e Encoder) AppendKey(dst []byte, key string) []byte {
	if len(dst) < 1 {
		dst = e.AppendBeginMarker(dst)
	}
	return e.AppendString(dst, key)
}