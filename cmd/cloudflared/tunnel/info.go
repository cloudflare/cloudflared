package tunnel

import (
	"time"

	"github.com/google/uuid"

	"github.com/cloudflare/cloudflared/tunnelstore"
)

type Info struct {
	ID         uuid.UUID                   `json:"id"`
	Name       string                      `json:"name"`
	CreatedAt  time.Time                   `json:"createdAt"`
	Connectors []*tunnelstore.ActiveClient `json:"conns"`
}
