package teamnet

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
)

// Route is a mapping from customer's IP space to a tunnel.
// Each route allows the customer to route eyeballs in their corporate network
// to certain private IP ranges. Each Route represents an IP range in their
// network, and says that eyeballs can reach that route using the corresponding
// tunnel.
type Route struct {
	Network   net.IPNet
	TunnelID  uuid.UUID
	Comment   string
	CreatedAt time.Time
	DeletedAt time.Time
}

// TableString outputs a table row summarizing the route, to be used
// when showing the user their routing table.
func (r Route) TableString() string {
	deletedColumn := "-"
	if !r.DeletedAt.IsZero() {
		deletedColumn = r.DeletedAt.Format(time.RFC3339)
	}
	return fmt.Sprintf(
		"%s\t%s\t%s\t%s\t%s\t",
		r.Network.String(),
		r.Comment,
		r.TunnelID,
		r.CreatedAt.Format(time.RFC3339),
		deletedColumn,
	)
}

// UnmarshalJSON handles fields with non-JSON types (e.g. net.IPNet).
func (r *Route) UnmarshalJSON(data []byte) error {

	// This is the raw JSON format that cloudflared receives from tunnelstore.
	// Note it does not understand types like IPNet.
	var resp struct {
		Network   string    `json:"network"`
		TunnelID  uuid.UUID `json:"tunnel_id"`
		Comment   string    `json:"comment"`
		CreatedAt time.Time `json:"created_at"`
		DeletedAt time.Time `json:"deleted_at"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}

	// Parse the raw JSON into a properly-typed response.
	_, network, err := net.ParseCIDR(resp.Network)
	if err != nil || network == nil {
		return fmt.Errorf("backend returned invalid network %s", resp.Network)
	}
	r.Network = *network
	r.TunnelID = resp.TunnelID
	r.Comment = resp.Comment
	r.CreatedAt = resp.CreatedAt
	r.DeletedAt = resp.DeletedAt
	return nil
}

// NewRoute has all the parameters necessary to add a new route to the table.
type NewRoute struct {
	Network  net.IPNet
	TunnelID uuid.UUID
	Comment  string
}

// MarshalJSON handles fields with non-JSON types (e.g. net.IPNet).
func (r NewRoute) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Network  string    `json:"network"`
		TunnelID uuid.UUID `json:"tunnel_id"`
		Comment  string    `json:"comment"`
	}{
		TunnelID: r.TunnelID,
		Comment:  r.Comment,
	})
}
