package tunnel

import (
	"time"

	"github.com/google/uuid"

	"github.com/kjake/cloudflared/cfapi"
)

type Info struct {
	ID         uuid.UUID             `json:"id"`
	Name       string                `json:"name"`
	CreatedAt  time.Time             `json:"createdAt"`
	Connectors []*cfapi.ActiveClient `json:"conns"`
}
