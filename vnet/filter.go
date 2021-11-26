package vnet

import (
	"net/url"
	"strconv"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var (
	filterId = cli.StringFlag{
		Name:  "id",
		Usage: "List virtual networks with the given `ID`",
	}
	filterName = cli.StringFlag{
		Name:  "name",
		Usage: "List virtual networks with the given `NAME`",
	}
	filterDefault = cli.BoolFlag{
		Name:  "is-default",
		Usage: "If true, lists the virtual network that is the default one. If false, lists all non-default virtual networks for the account. If absent, all are included in the results regardless of their default status.",
	}
	filterDeleted = cli.BoolFlag{
		Name:  "show-deleted",
		Usage: "If false (default), only show non-deleted virtual networks. If true, only show deleted virtual networks.",
	}
	FilterFlags = []cli.Flag{
		&filterId,
		&filterName,
		&filterDefault,
		&filterDeleted,
	}
)

// Filter which virtual networks get queried.
type Filter struct {
	queryParams url.Values
}

func NewFilter() *Filter {
	return &Filter{
		queryParams: url.Values{},
	}
}

func (f *Filter) ById(vnetId uuid.UUID) {
	f.queryParams.Set("id", vnetId.String())
}

func (f *Filter) ByName(name string) {
	f.queryParams.Set("name", name)
}

func (f *Filter) ByDefaultStatus(isDefault bool) {
	f.queryParams.Set("is_default", strconv.FormatBool(isDefault))
}

func (f *Filter) WithDeleted(isDeleted bool) {
	f.queryParams.Set("is_deleted", strconv.FormatBool(isDeleted))
}

func (f *Filter) MaxFetchSize(max uint) {
	f.queryParams.Set("per_page", strconv.Itoa(int(max)))
}

func (f Filter) Encode() string {
	return f.queryParams.Encode()
}

// NewFromCLI parses CLI flags to discover which filters should get applied to list virtual networks.
func NewFromCLI(c *cli.Context) (*Filter, error) {
	f := NewFilter()

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
