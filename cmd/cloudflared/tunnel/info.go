package tunnel

import (
	"time"

	"github.com/cloudflare/cloudflared/tunnelstore"
	"github.com/google/uuid"
)

type Info struct {
	ID         uuid.UUID                   `json:"id"`
	Name       string                      `json:"name"`
	CreatedAt  time.Time                   `json:"createdAt"`
	Connectors []*tunnelstore.ActiveClient `json:"conns"`
}
