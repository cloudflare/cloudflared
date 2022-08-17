package packet

import "github.com/google/uuid"

type Session struct {
	ID      uuid.UUID
	Payload []byte
}
