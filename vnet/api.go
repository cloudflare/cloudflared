package vnet

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type NewVirtualNetwork struct {
	Name      string `json:"name"`
	Comment   string `json:"comment"`
	IsDefault bool   `json:"is_default"`
}

type VirtualNetwork struct {
	ID        uuid.UUID `json:"id"`
	Comment   string    `json:"comment"`
	Name      string    `json:"name"`
	IsDefault bool      `json:"is_default_network"`
	CreatedAt time.Time `json:"created_at"`
	DeletedAt time.Time `json:"deleted_at"`
}

type UpdateVirtualNetwork struct {
	Name      *string `json:"name,omitempty"`
	Comment   *string `json:"comment,omitempty"`
	IsDefault *bool   `json:"is_default_network,omitempty"`
}

func (virtualNetwork VirtualNetwork) TableString() string {
	deletedColumn := "-"
	if !virtualNetwork.DeletedAt.IsZero() {
		deletedColumn = virtualNetwork.DeletedAt.Format(time.RFC3339)
	}
	return fmt.Sprintf(
		"%s\t%s\t%s\t%s\t%s\t%s\t",
		virtualNetwork.ID,
		virtualNetwork.Name,
		strconv.FormatBool(virtualNetwork.IsDefault),
		virtualNetwork.Comment,
		virtualNetwork.CreatedAt.Format(time.RFC3339),
		deletedColumn,
	)
}
