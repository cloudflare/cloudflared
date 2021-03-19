package tunnelstore

import (
	"net/url"

	"github.com/google/uuid"
)

type CleanupParams struct {
	queryParams url.Values
}

func NewCleanupParams() *CleanupParams {
	return &CleanupParams{
		queryParams: url.Values{},
	}
}

func (cp *CleanupParams) ForClient(clientID uuid.UUID) {
	cp.queryParams.Set("client_id", clientID.String())
}

func (cp CleanupParams) encode() string {
	return cp.queryParams.Encode()
}
