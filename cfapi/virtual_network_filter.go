package cfapi

import (
	"net/url"
	"strconv"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var (
	filterVnetId = cli.StringFlag{
		Name:  "id",
		Usage: "List virtual networks with the given `ID`",
	}
	filterVnetByName = cli.StringFlag{
		Name:  "name",
		Usage: "List virtual networks with the given `NAME`",
	}
	filterDefaultVnet = cli.BoolFlag{
		Name:  "is-default",
		Usage: "If true, lists the virtual network that is the default one. If false, lists all non-default virtual networks for the account. If absent, all are included in the results regardless of their default status.",
	}
	filterDeletedVnet = cli.BoolFlag{
		Name:  "show-deleted",
		Usage: "If false (default), only show non-deleted virtual networks. If true, only show deleted virtual networks.",
	}
	VnetFilterFlags = []cli.Flag{
		&filterVnetId,
		&filterVnetByName,
		&filterDefaultVnet,
		&filterDeletedVnet,
	}
)

// VnetFilter which virtual networks get queried.
type VnetFilter struct {
	queryParams url.Values
}

func NewVnetFilter() *VnetFilter {
	return &VnetFilter{
		queryParams: url.Values{},
	}
}

func (f *VnetFilter) ById(vnetId uuid.UUID) {
	f.queryParams.Set("id", vnetId.String())
}

func (f *VnetFilter) ByName(name string) {
	f.queryParams.Set("name", name)
}

func (f *VnetFilter) ByDefaultStatus(isDefault bool) {
	f.queryParams.Set("is_default", strconv.FormatBool(isDefault))
}

func (f *VnetFilter) WithDeleted(isDeleted bool) {
	f.queryParams.Set("is_deleted", strconv.FormatBool(isDeleted))
}

func (f *VnetFilter) MaxFetchSize(max uint) {
	f.queryParams.Set("per_page", strconv.Itoa(int(max)))
}

func (f VnetFilter) Encode() string {
	return f.queryParams.Encode()
}

// NewFromCLI parses CLI flags to discover which filters should get applied to list virtual networks.
func NewFromCLI(c *cli.Context) (*VnetFilter, error) {
	f := NewVnetFilter()

	if id := c.String("id"); id != "" {
		vnetId, err := uuid.Parse(id)
		if err != nil {
			return nil, errors.Wrapf(err, "%s is not a valid virtual network ID", id)
		}
		f.ById(vnetId)
	}

	if name := c.String("name"); name != "" {
		f.ByName(name)
	}

	if c.IsSet("is-default") {
		f.ByDefaultStatus(c.Bool("is-default"))
	}

	f.WithDeleted(c.Bool("show-deleted"))

	if maxFetch := c.Int("max-fetch-size"); maxFetch > 0 {
		f.MaxFetchSize(uint(maxFetch))
	}

	return f, nil
}
