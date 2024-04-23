package cfapi

import (
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
)

const (
	TimeLayout = time.RFC3339
)

type TunnelFilter struct {
	queryParams url.Values
}

func NewTunnelFilter() *TunnelFilter {
	return &TunnelFilter{
		queryParams: url.Values{},
	}
}

func (f *TunnelFilter) ByName(name string) {
	f.queryParams.Set("name", name)
}

func (f *TunnelFilter) ByNamePrefix(namePrefix string) {
	f.queryParams.Set("name_prefix", namePrefix)
}

func (f *TunnelFilter) ExcludeNameWithPrefix(excludePrefix string) {
	f.queryParams.Set("exclude_prefix", excludePrefix)
}

func (f *TunnelFilter) NoDeleted() {
	f.queryParams.Set("is_deleted", "false")
}

func (f *TunnelFilter) ByExistedAt(existedAt time.Time) {
	f.queryParams.Set("existed_at", existedAt.Format(TimeLayout))
}

func (f *TunnelFilter) ByTunnelID(tunnelID uuid.UUID) {
	f.queryParams.Set("uuid", tunnelID.String())
}

func (f *TunnelFilter) MaxFetchSize(max uint) {
	f.queryParams.Set("per_page", strconv.Itoa(int(max)))
}

func (f *TunnelFilter) Page(page int) {
	f.queryParams.Set("page", strconv.Itoa(page))
}

func (f TunnelFilter) encode() string {
	return f.queryParams.Encode()
}
