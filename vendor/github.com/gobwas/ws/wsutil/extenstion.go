package wsutil

import "github.com/gobwas/ws"

// RecvExtension is an interface for clearing fragment header RSV bits.
type RecvExtension interface {
	UnsetBits(ws.Header) (ws.Header, error)
}

// RecvExtensionFunc is an adapter to allow the use of ordinary functions as
// RecvExtension.
type RecvExtensionFunc func(ws.Header) (ws.Header, error)

// BitsRecv implements RecvExtension.
func (fn RecvExtensionFunc) UnsetBits(h ws.Header) (ws.Header, error) {
	return fn(h)
}

// SendExtension is an interface for setting fragment header RSV bits.
type SendExtension interface {
	SetBits(ws.Header) (ws.Header, error)
}

// SendExtensionFunc is an adapter to allow the use of ordinary functions as
// SendExtension.
type SendExtensionFunc func(ws.Header) (ws.Header, error)

// BitsSend implements SendExtension.
func (fn SendExtensionFunc) SetBits(h ws.Header) (ws.Header, error) {
	return fn(h)
}
