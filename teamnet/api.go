package teamnet

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// Route is a mapping from customer's IP space to a tunnel.
// Each route allows the customer to route eyeballs in their corporate network
// to certain private IP ranges. Each Route represents an IP range in their
// network, and says that eyeballs can reach that route using the corresponding
// tunnel.
type Route struct {
	Network   CIDR      `json:"network"`
	TunnelID  uuid.UUID `json:"tunnel_id"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
	DeletedAt time.Time `json:"deleted_at"`
}

// CIDR is just a newtype wrapper around net.IPNet. It adds JSON unmarshalling.
type CIDR net.IPNet

func (c CIDR) String() string {
	n := net.IPNet(c)
	return n.String()
}

func (c CIDR) MarshalJSON() ([]byte, error) {
	str := c.String()
	json, err := json.Marshal(str)
	if err != nil {
		return nil, errors.Wrap(err, "error serializing CIDR into JSON")
	}
	return json, nil
}

// UnmarshalJSON parses a JSON string into net.IPNet
func (c *CIDR) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.Wrap(err, "error parsing cidr string")
	}
	_, network, err := net.ParseCIDR(s)
	if err != nil {
		return errors.Wrap(err, "error parsing invalid network from backend")
	}
	if network == nil {
		return fmt.Errorf("backend returned invalid network %s", s)
	}
	*c = CIDR(*network)
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
		TunnelID uuid.UUID `json:"tunnel_id"`
		Comment  string    `json:"comment"`
	}{
		TunnelID: r.TunnelID,
		Comment:  r.Comment,
	})
}

// DetailedRoute is just a Route with some extra fields, e.g. TunnelName.
type DetailedRoute struct {
	Network    CIDR      `json:"network"`
	TunnelID   uuid.UUID `json:"tunnel_id"`
	Comment    string    `json:"comment"`
	CreatedAt  time.Time `json:"created_at"`
	DeletedAt  time.Time `json:"deleted_at"`
	TunnelName string    `json:"tunnel_name"`
}

// IsZero checks if DetailedRoute is the zero value.
func (r *DetailedRoute) IsZero() bool {
	return r.TunnelID == uuid.Nil
}

// TableString outputs a table row summarizing the route, to be used
// when showing the user their routing table.
func (r DetailedRoute) TableString() string {
	deletedColumn := "-"
	if !r.DeletedAt.IsZero() {
		deletedColumn = r.DeletedAt.Format(time.RFC3339)
	}
	return fmt.Sprintf(
		"%s\t%s\t%s\t%s\t%s\t%s\t",
		r.Network.String(),
		r.Comment,
		r.TunnelID,
		r.TunnelName,
		r.CreatedAt.Format(time.RFC3339),
		deletedColumn,
	)
}
