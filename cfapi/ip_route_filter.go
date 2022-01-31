package cfapi

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

var (
	filterIpRouteDeleted = cli.BoolFlag{
		Name:  "filter-is-deleted",
		Usage: "If false (default), only show non-deleted routes. If true, only show deleted routes.",
	}
	filterIpRouteTunnelID = cli.StringFlag{
		Name:  "filter-tunnel-id",
		Usage: "Show only routes with the given tunnel ID.",
	}
	filterSubsetIpRoute = cli.StringFlag{
		Name:    "filter-network-is-subset-of",
		Aliases: []string{"nsub"},
		Usage:   "Show only routes whose network is a subset of the given network.",
	}
	filterSupersetIpRoute = cli.StringFlag{
		Name:    "filter-network-is-superset-of",
		Aliases: []string{"nsup"},
		Usage:   "Show only routes whose network is a superset of the given network.",
	}
	filterIpRouteComment = cli.StringFlag{
		Name:  "filter-comment-is",
		Usage: "Show only routes with this comment.",
	}
	filterIpRouteByVnet = cli.StringFlag{
		Name:  "filter-vnet-id",
		Usage: "Show only routes that are attached to the given virtual network ID.",
	}

	// Flags contains all filter flags.
	IpRouteFilterFlags = []cli.Flag{
		&filterIpRouteDeleted,
		&filterIpRouteTunnelID,
		&filterSubsetIpRoute,
		&filterSupersetIpRoute,
		&filterIpRouteComment,
		&filterIpRouteByVnet,
	}
)

// IpRouteFilter which routes get queried.
type IpRouteFilter struct {
	queryParams url.Values
}

// NewIpRouteFilterFromCLI parses CLI flags to discover which filters should get applied.
func NewIpRouteFilterFromCLI(c *cli.Context) (*IpRouteFilter, error) {
	f := &IpRouteFilter{
		queryParams: url.Values{},
	}

	// Set deletion filter
	if flag := filterIpRouteDeleted.Name; c.IsSet(flag) && c.Bool(flag) {
		f.deleted()
	} else {
		f.notDeleted()
	}

	if subset, err := cidrFromFlag(c, filterSubsetIpRoute); err != nil {
		return nil, err
	} else if subset != nil {
		f.networkIsSupersetOf(*subset)
	}

	if superset, err := cidrFromFlag(c, filterSupersetIpRoute); err != nil {
		return nil, err
	} else if superset != nil {
		f.networkIsSupersetOf(*superset)
	}

	if comment := c.String(filterIpRouteComment.Name); comment != "" {
		f.commentIs(comment)
	}

	if tunnelID := c.String(filterIpRouteTunnelID.Name); tunnelID != "" {
		u, err := uuid.Parse(tunnelID)
		if err != nil {
			return nil, errors.Wrapf(err, "Couldn't parse UUID from %s", filterIpRouteTunnelID.Name)
		}
		f.tunnelID(u)
	}

	if vnetId := c.String(filterIpRouteByVnet.Name); vnetId != "" {
		u, err := uuid.Parse(vnetId)
		if err != nil {
			return nil, errors.Wrapf(err, "Couldn't parse UUID from %s", filterIpRouteByVnet.Name)
		}
		f.vnetID(u)
	}

	if maxFetch := c.Int("max-fetch-size"); maxFetch > 0 {
		f.MaxFetchSize(uint(maxFetch))
	}

	return f, nil
}

// Parses a CIDR from the flag. If the flag was unset, returns (nil, nil).
func cidrFromFlag(c *cli.Context, flag cli.StringFlag) (*net.IPNet, error) {
	if !c.IsSet(flag.Name) {
		return nil, nil
	}

	_, subset, err := net.ParseCIDR(c.String(flag.Name))
	if err != nil {
		return nil, err
	} else if subset == nil {
		return nil, fmt.Errorf("Invalid CIDR supplied for %s", flag.Name)
	}

	return subset, nil
}

func (f *IpRouteFilter) commentIs(comment string) {
	f.queryParams.Set("comment", comment)
}

func (f *IpRouteFilter) notDeleted() {
	f.queryParams.Set("is_deleted", "false")
}

func (f *IpRouteFilter) deleted() {
	f.queryParams.Set("is_deleted", "true")
}

func (f *IpRouteFilter) networkIsSubsetOf(superset net.IPNet) {
	f.queryParams.Set("network_subset", superset.String())
}

func (f *IpRouteFilter) networkIsSupersetOf(subset net.IPNet) {
	f.queryParams.Set("network_superset", subset.String())
}

func (f *IpRouteFilter) existedAt(existedAt time.Time) {
	f.queryParams.Set("existed_at", existedAt.Format(time.RFC3339))
}

func (f *IpRouteFilter) tunnelID(id uuid.UUID) {
	f.queryParams.Set("tunnel_id", id.String())
}

func (f *IpRouteFilter) vnetID(id uuid.UUID) {
	f.queryParams.Set("virtual_network_id", id.String())
}

func (f *IpRouteFilter) MaxFetchSize(max uint) {
	f.queryParams.Set("per_page", strconv.Itoa(int(max)))
}

func (f IpRouteFilter) Encode() string {
	return f.queryParams.Encode()
}
