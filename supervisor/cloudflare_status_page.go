package supervisor

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudflare/golibs/lrucache"
)

// StatusPage.io API docs:
// https://www.cloudflarestatus.com/api/v2/#incidents-unresolved
const (
	activeIncidentsURL    = "https://yh6f0r4529hb.statuspage.io/api/v2/incidents/unresolved.json"
	argoTunnelKeyword     = "argo tunnel"
	incidentDetailsPrefix = "https://www.cloudflarestatus.com/incidents/"
)

// IncidentLookup is an object that checks for active incidents in
// the Cloudflare infrastructure.
type IncidentLookup interface {
	ActiveIncidents() []Incident
}

// NewIncidentLookup returns a new IncidentLookup instance that caches its
// results with a 1-minute TTL.
func NewIncidentLookup() IncidentLookup {
	return newCachedIncidentLookup(fetchActiveIncidents)
}

type IncidentUpdate struct {
	Body string
}

type Incident struct {
	Name    string
	ID      string           `json:"id"`
	Updates []IncidentUpdate `json:"incident_updates"`
}

type StatusPage struct {
	Incidents []Incident
}

func (i Incident) URL() string {
	return incidentDetailsPrefix + i.ID
}

func parseStatusPage(data []byte) (*StatusPage, error) {
	var result StatusPage
	err := json.Unmarshal(data, &result)
	return &result, err
}

func isArgoTunnelIncident(i Incident) bool {
	if strings.Contains(strings.ToLower(i.Name), argoTunnelKeyword) {
		return true
	}
	for _, u := range i.Updates {
		if strings.Contains(strings.ToLower(u.Body), argoTunnelKeyword) {
			return true
		}
	}
	return false
}

func fetchActiveIncidents() (incidents []Incident) {
	resp, err := http.Get(activeIncidentsURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	statusPage, err := parseStatusPage(body)
	if err != nil {
		return
	}
	for _, i := range statusPage.Incidents {
		if isArgoTunnelIncident(i) {
			incidents = append(incidents, i)
		}
	}
	return incidents
}

type cachedIncidentLookup struct {
	cache          *lrucache.LRUCache
	ttl            time.Duration
	uncachedLookup func() []Incident
}

func newCachedIncidentLookup(uncachedLookup func() []Incident) *cachedIncidentLookup {
	return &cachedIncidentLookup{
		cache:          lrucache.NewLRUCache(1),
		ttl:            time.Minute,
		uncachedLookup: uncachedLookup,
	}
}

// We only need one cache entry. Always use the empty string as its key.
const cacheKey = ""

func (c *cachedIncidentLookup) ActiveIncidents() []Incident {
	if cached, ok := c.cache.GetNotStale(cacheKey); ok {
		if incidents, ok := cached.([]Incident); ok {
			return incidents
		}
	}
	incidents := c.uncachedLookup()
	c.cache.Set(cacheKey, incidents, time.Now().Add(c.ttl))
	return incidents
}
