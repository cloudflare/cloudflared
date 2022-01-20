package cfapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type Change = string

const (
	ChangeNew       = "new"
	ChangeUpdated   = "updated"
	ChangeUnchanged = "unchanged"
)

// HostnameRoute represents a record type that can route to a tunnel
type HostnameRoute interface {
	json.Marshaler
	RecordType() string
	UnmarshalResult(body io.Reader) (HostnameRouteResult, error)
	String() string
}

type HostnameRouteResult interface {
	// SuccessSummary explains what will route to this tunnel when it's provisioned successfully
	SuccessSummary() string
}

type DNSRoute struct {
	userHostname      string
	overwriteExisting bool
}

type DNSRouteResult struct {
	route *DNSRoute
	CName Change `json:"cname"`
	Name  string `json:"name"`
}

func NewDNSRoute(userHostname string, overwriteExisting bool) HostnameRoute {
	return &DNSRoute{
		userHostname:      userHostname,
		overwriteExisting: overwriteExisting,
	}
}

func (dr *DNSRoute) MarshalJSON() ([]byte, error) {
	s := struct {
		Type              string `json:"type"`
		UserHostname      string `json:"user_hostname"`
		OverwriteExisting bool   `json:"overwrite_existing"`
	}{
		Type:              dr.RecordType(),
		UserHostname:      dr.userHostname,
		OverwriteExisting: dr.overwriteExisting,
	}
	return json.Marshal(&s)
}

func (dr *DNSRoute) UnmarshalResult(body io.Reader) (HostnameRouteResult, error) {
	var result DNSRouteResult
	err := parseResponse(body, &result)
	result.route = dr
	return &result, err
}

func (dr *DNSRoute) RecordType() string {
	return "dns"
}

func (dr *DNSRoute) String() string {
	return fmt.Sprintf("%s %s", dr.RecordType(), dr.userHostname)
}

func (res *DNSRouteResult) SuccessSummary() string {
	var msgFmt string
	switch res.CName {
	case ChangeNew:
		msgFmt = "Added CNAME %s which will route to this tunnel"
	case ChangeUpdated: // this is not currently returned by tunnelsore
		msgFmt = "%s updated to route to your tunnel"
	case ChangeUnchanged:
		msgFmt = "%s is already configured to route to your tunnel"
	}
	return fmt.Sprintf(msgFmt, res.hostname())
}

// hostname yields the resulting name for the DNS route; if that is not available from Cloudflare API, then the
// requested name is returned instead (should not be the common path, it is just a fall-back).
func (res *DNSRouteResult) hostname() string {
	if res.Name != "" {
		return res.Name
	}
	return res.route.userHostname
}

type LBRoute struct {
	lbName string
	lbPool string
}

type LBRouteResult struct {
	route        *LBRoute
	LoadBalancer Change `json:"load_balancer"`
	Pool         Change `json:"pool"`
}

func NewLBRoute(lbName, lbPool string) HostnameRoute {
	return &LBRoute{
		lbName: lbName,
		lbPool: lbPool,
	}
}

func (lr *LBRoute) MarshalJSON() ([]byte, error) {
	s := struct {
		Type   string `json:"type"`
		LBName string `json:"lb_name"`
		LBPool string `json:"lb_pool"`
	}{
		Type:   lr.RecordType(),
		LBName: lr.lbName,
		LBPool: lr.lbPool,
	}
	return json.Marshal(&s)
}

func (lr *LBRoute) RecordType() string {
	return "lb"
}

func (lb *LBRoute) String() string {
	return fmt.Sprintf("%s %s %s", lb.RecordType(), lb.lbName, lb.lbPool)
}

func (lr *LBRoute) UnmarshalResult(body io.Reader) (HostnameRouteResult, error) {
	var result LBRouteResult
	err := parseResponse(body, &result)
	result.route = lr
	return &result, err
}

func (res *LBRouteResult) SuccessSummary() string {
	var msg string
	switch res.LoadBalancer + "," + res.Pool {
	case "new,new":
		msg = "Created load balancer %s and added a new pool %s with this tunnel as an origin"
	case "new,updated":
		msg = "Created load balancer %s with an existing pool %s which was updated to use this tunnel as an origin"
	case "new,unchanged":
		msg = "Created load balancer %s with an existing pool %s which already has this tunnel as an origin"
	case "updated,new":
		msg = "Added new pool %[2]s with this tunnel as an origin to load balancer %[1]s"
	case "updated,updated":
		msg = "Updated pool %[2]s to use this tunnel as an origin and added it to load balancer %[1]s"
	case "updated,unchanged":
		msg = "Added pool %[2]s, which already has this tunnel as an origin, to load balancer %[1]s"
	case "unchanged,updated":
		msg = "Added this tunnel as an origin in pool %[2]s which is already used by load balancer %[1]s"
	case "unchanged,unchanged":
		msg = "Load balancer %s already uses pool %s which has this tunnel as an origin"
	case "unchanged,new":
		// this state is not possible
		fallthrough
	default:
		msg = "Something went wrong: failed to modify load balancer %s with pool %s; please check traffic manager configuration in the dashboard"
	}

	return fmt.Sprintf(msg, res.route.lbName, res.route.lbPool)
}

func (r *RESTClient) RouteTunnel(tunnelID uuid.UUID, route HostnameRoute) (HostnameRouteResult, error) {
	endpoint := r.baseEndpoints.zoneLevel
	endpoint.Path = path.Join(endpoint.Path, fmt.Sprintf("%v/routes", tunnelID))
	resp, err := r.sendRequest("PUT", endpoint, route)
	if err != nil {
		return nil, errors.Wrap(err, "REST request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return route.UnmarshalResult(resp.Body)
	}

	return nil, r.statusCodeToError("add route", resp)
}
