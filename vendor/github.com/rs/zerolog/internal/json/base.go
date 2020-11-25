package json

type Encoder struct{}

// AppendKey appends a new key to the output JSON.
func (e Encoder) AppendKey(dst []byte, key string) []byte {
	if dst[len(dst)-1] != '{' {
		dst = append(dst, ',')
	}
	return append(e.AppendString(dst, key), ':')
}
