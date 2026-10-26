//go:build gomock || generate

package mocks

//go:generate sh -c "go run go.uber.org/mock/mockgen -typed -build_flags=\"-tags=gomock\" -package mocks -destination mock_limiter.go -source=../flow/limiter.go Limiter"

//go:generate sh -c "go run go.uber.org/mock/mockgen -typed -build_flags=\"-tags=gomock\" -package mocks -destination mock_resolvers.go -source=../prechecks/resolvers.go"

//go:generate sh -c "go run go.uber.org/mock/mockgen -typed -build_flags=\"-tags=gomock\" -package mocks -destination mock_quic_connection.go -source=../quic/quic_connection.go"
