//go:build gomock || generate

package handshake

//go:generate sh -c "go run github.com/golang/mock/mockgen -build_flags=\"-tags=gomock\"  -package handshake -destination mock_handshake_runner_test.go github.com/quic-go/quic-go/internal/handshake HandshakeRunner"
type HandshakeRunner = handshakeRunner
