package tunnelstore

import (
	"net/url"
	"time"

	"github.com/google/uuid"
)

const (
	TimeLayout = time.RFC3339
)

type Filter struct {
	queryParams url.Values
}

func NewFilter() *Filter {
	return &Filter{
		queryParams: url.Values{},
	}
}

func (f *Filter) ByName(name string) {
	f.queryParams.Set("name", name)
}

func (f *Filter) ShowDeleted() {
	f.queryParams.Set("is_deleted", "false")
}

func (f *Filter) ByExistedAt(existedAt time.Time) {
	f.queryParams.Set("existed_at", existedAt.Format(TimeLayout))
}

func (f *Filter) ByTunnelID(tunnelID uuid.UUID) {
	f.queryParams.Set("uuid", tunnelID.String())
}

func (f Filter) encode() string {
	return f.queryParams.Encode()
}
