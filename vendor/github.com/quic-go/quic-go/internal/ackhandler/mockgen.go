//go:build gomock || generate

package ackhandler

//go:generate sh -c "go run github.com/golang/mock/mockgen -build_flags=\"-tags=gomock\"  -package ackhandler -destination mock_sent_packet_tracker_test.go github.com/quic-go/quic-go/internal/ackhandler SentPacketTracker"
type SentPacketTracker = sentPacketTracker
