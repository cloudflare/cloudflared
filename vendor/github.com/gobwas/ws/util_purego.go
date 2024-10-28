//go:build purego
// +build purego

package ws

func strToBytes(str string) (bts []byte) {
	return []byte(str)
}

func btsToString(bts []byte) (str string) {
	return string(bts)
}
