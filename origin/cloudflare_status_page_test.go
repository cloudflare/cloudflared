package origin

import (
	"testing"
	"time"

	"github.com/cloudflare/golibs/lrucache"
	"github.com/stretchr/testify/assert"
)

func TestParseStatusPage(t *testing.T) {
	testCases := []struct {
		input  []byte
		output *StatusPage
		fail   bool
	}{
		{
			input: []byte(`<html>
				<head><title>504 Gateway Time-out</title></head>
				<body><center><h1>504 Gateway Time-out</h1></center></body>
			</html>`),
			output: nil,
			fail:   true,
		},
		{
			input: []byte(`{
				"page": {
					"id": "yh6f0r4529hb",
					"name": "Cloudflare",
					"url": "https://www.cloudflarestatus.com",
					"time_zone": "Etc/UTC",
					"updated_at": "2019-01-10T20:11:38.750Z"
				},
				"incidents": [
					{
						"name": "Cloudflare API service issues",
						"status": "resolved",
						"created_at": "2018-09-17T19:29:21.132Z",
						"updated_at": "2018-09-18T07:45:41.313Z",
						"monitoring_at": "2018-09-17T21:35:06.492Z",
						"resolved_at": "2018-09-18T07:45:41.290Z",
						"shortlink": "http://stspg.io/7f079791e",
						"id": "q746ybtyb6q0",
						"page_id": "yh6f0r4529hb",
						"incident_updates": [
							{
								"status": "resolved",
								"body": "Cloudflare has resolved the issue and the service have resumed normal operation.",
								"created_at": "2018-09-18T07:45:41.290Z",
								"updated_at": "2018-09-18T07:45:41.290Z",
								"display_at": "2018-09-18T07:45:41.290Z",
								"affected_components": [
									{
										"code": "g4tb35rs9yw7",
										"name": "Cloudflare customer dashboard and APIs - Cloudflare APIs",
										"old_status": "operational",
										"new_status": "operational"
									}
								],
								"deliver_notifications": true,
								"tweet_id": null,
								"id": "zl5g2pl5zhfs",
								"incident_id": "q746ybtyb6q0",
								"custom_tweet": null
							},
							{
								"status": "monitoring",
								"body": "Cloudflare has implemented a fix for this issue and is currently monitoring the results.\r\n\r\nWe will update the status once the issue is resolved.",
								"created_at": "2018-09-17T21:35:06.492Z",
								"updated_at": "2018-09-17T21:35:06.492Z",
								"display_at": "2018-09-17T21:35:06.492Z",
								"affected_components": [
									{
										"code": "g4tb35rs9yw7",
										"name": "Cloudflare customer dashboard and APIs - Cloudflare APIs",
										"old_status": "degraded_performance",
										"new_status": "operational"
									}
								],
								"deliver_notifications": false,
								"tweet_id": null,
								"id": "0001sv3chdnx",
								"incident_id": "q746ybtyb6q0",
								"custom_tweet": null
							},
							{
								"status": "investigating",
								"body": "We are continuing to investigate this issue.",
								"created_at": "2018-09-17T19:30:08.049Z",
								"updated_at": "2018-09-17T19:30:08.049Z",
								"display_at": "2018-09-17T19:30:08.049Z",
								"affected_components": [
									{
										"code": "g4tb35rs9yw7",
										"name": "Cloudflare customer dashboard and APIs - Cloudflare APIs",
										"old_status": "operational",
										"new_status": "degraded_performance"
									}
								],
								"deliver_notifications": false,
								"tweet_id": null,
								"id": "qdr164tfpq7m",
								"incident_id": "q746ybtyb6q0",
								"custom_tweet": null
							},
							{
								"status": "investigating",
								"body": "Cloudflare is investigating issues with APIs and Page Rule delays for Page Rule updates.  Cloudflare Page Rule service delivery is unaffected and is operating normally.  Also, these issues do not affect the Cloudflare CDN and therefore, do not impact customer websites.",
								"created_at": "2018-09-17T19:29:21.201Z",
								"updated_at": "2018-09-17T19:29:21.201Z",
								"display_at": "2018-09-17T19:29:21.201Z",
								"affected_components": [
									{
										"code": "g4tb35rs9yw7",
										"name": "Cloudflare customer dashboard and APIs - Cloudflare APIs",
										"old_status": "operational",
										"new_status": "operational"
									}
								],
								"deliver_notifications": false,
								"tweet_id": null,
								"id": "qzl2n0q8tskg",
								"incident_id": "q746ybtyb6q0",
								"custom_tweet": null
							}
						],
						"components": [
							{
								"status": "operational",
								"name": "Cloudflare APIs",
								"created_at": "2014-10-09T03:32:07.158Z",
								"updated_at": "2019-01-01T22:58:30.846Z",
								"position": 2,
								"description": null,
								"showcase": false,
								"id": "g4tb35rs9yw7",
								"page_id": "yh6f0r4529hb",
								"group_id": "1km35smx8p41",
								"group": false,
								"only_show_if_degraded": false
							}
						],
						"impact": "minor"
					},
					{
						"name": "Web Analytics Delays",
						"status": "resolved",
						"created_at": "2018-09-17T18:05:39.907Z",
						"updated_at": "2018-09-17T22:53:05.078Z",
						"monitoring_at": null,
						"resolved_at": "2018-09-17T22:53:05.057Z",
						"shortlink": "http://stspg.io/cb208928c",
						"id": "wqfk9mzs5qt1",
						"page_id": "yh6f0r4529hb",
						"incident_updates": [
							{
								"status": "resolved",
								"body": "Cloudflare has resolved the issue and Web Analytics have resumed normal operation.",
								"created_at": "2018-09-17T22:53:05.057Z",
								"updated_at": "2018-09-17T22:53:05.057Z",
								"display_at": "2018-09-17T22:53:05.057Z",
								"affected_components": [
									{
										"code": "4c231tkdlpcl",
										"name": "Cloudflare customer dashboard and APIs - Analytics",
										"old_status": "degraded_performance",
										"new_status": "operational"
									}
								],
								"deliver_notifications": false,
								"tweet_id": null,
								"id": "93y1w00yqzk4",
								"incident_id": "wqfk9mzs5qt1",
								"custom_tweet": null
							},
							{
								"status": "investigating",
								"body": "There is a delay in processing Cloudflare Web Analytics. This affects timely delivery of customer data.\n\nThese delays do not impact analytics for DNS and Rate Limiting.",
								"created_at": "2018-09-17T18:05:40.033Z",
								"updated_at": "2018-09-17T18:05:40.033Z",
								"display_at": "2018-09-17T18:05:40.033Z",
								"affected_components": [
									{
										"code": "4c231tkdlpcl",
										"name": "Cloudflare customer dashboard and APIs - Analytics",
										"old_status": "operational",
										"new_status": "degraded_performance"
									}
								],
								"deliver_notifications": false,
								"tweet_id": null,
								"id": "362t6lv0vrpk",
								"incident_id": "wqfk9mzs5qt1",
								"custom_tweet": null
							}
						],
						"components": [
							{
								"status": "operational",
								"name": "Analytics",
								"created_at": "2014-11-13T11:54:10.191Z",
								"updated_at": "2018-12-31T08:20:52.349Z",
								"position": 3,
								"description": "Customer data",
								"showcase": false,
								"id": "4c231tkdlpcl",
								"page_id": "yh6f0r4529hb",
								"group_id": "1km35smx8p41",
								"group": false,
								"only_show_if_degraded": false
							}
						],
						"impact": "minor"
					}
				]
			}`),
			output: &StatusPage{
				Incidents: []Incident{
					Incident{
						Name: "Cloudflare API service issues",
						ID:   "q746ybtyb6q0",
						Updates: []IncidentUpdate{
							IncidentUpdate{
								Body: "Cloudflare has resolved the issue and the service have resumed normal operation.",
							},
							IncidentUpdate{
								Body: "Cloudflare has implemented a fix for this issue and is currently monitoring the results.\r\n\r\nWe will update the status once the issue is resolved.",
							},
							IncidentUpdate{
								Body: "We are continuing to investigate this issue.",
							},
							IncidentUpdate{
								Body: "Cloudflare is investigating issues with APIs and Page Rule delays for Page Rule updates.  Cloudflare Page Rule service delivery is unaffected and is operating normally.  Also, these issues do not affect the Cloudflare CDN and therefore, do not impact customer websites.",
							},
						},
					},
					Incident{
						Name: "Web Analytics Delays",
						ID:   "wqfk9mzs5qt1",
						Updates: []IncidentUpdate{
							IncidentUpdate{
								Body: "Cloudflare has resolved the issue and Web Analytics have resumed normal operation.",
							},
							IncidentUpdate{
								Body: "There is a delay in processing Cloudflare Web Analytics. This affects timely delivery of customer data.\n\nThese delays do not impact analytics for DNS and Rate Limiting.",
							},
						},
					},
				},
			},
			fail: false,
		},
	}

	for _, testCase := range testCases {
		output, err := parseStatusPage(testCase.input)
		if testCase.fail {
			assert.Error(t, err)
		} else {
			assert.Nil(t, err)
			assert.Equal(t, testCase.output, output)
		}
	}
}

func TestIsArgoTunnelIncident(t *testing.T) {
	testCases := []struct {
		input  Incident
		output bool
	}{
		{
			input:  Incident{},
			output: false,
		},
		{
			input:  Incident{Name: "An Argo Tunnel incident"},
			output: true,
		},
		{
			input:  Incident{Name: "an argo tunnel incident"},
			output: true,
		},
		{
			input:  Incident{Name: "an aRgO TuNnEl incident"},
			output: true,
		},
		{
			input:  Incident{Name: "an argotunnel incident"},
			output: false,
		},
		{
			input:  Incident{Name: "irrelevant"},
			output: false,
		},
		{
			input: Incident{
				Name: "irrelevant",
				Updates: []IncidentUpdate{
					IncidentUpdate{Body: "irrelevant"},
					IncidentUpdate{Body: "an Argo Tunnel incident"},
					IncidentUpdate{Body: "irrelevant"},
				},
			},
			output: true,
		},
		{
			input: Incident{
				Name: "an Argo Tunnel incident",
				Updates: []IncidentUpdate{
					IncidentUpdate{Body: "irrelevant"},
					IncidentUpdate{Body: "irrelevant"},
					IncidentUpdate{Body: "irrelevant"},
				},
			},
			output: true,
		},
	}
	for _, testCase := range testCases {
		actual := isArgoTunnelIncident(testCase.input)
		assert.Equal(t, testCase.output, actual, "Test case failed: %v", testCase.input)
	}
}

func TestIncidentURL(t *testing.T) {
	incident := Incident{
		ID: "s6k0dnn5347b",
	}
	assert.Equal(t, "https://www.cloudflarestatus.com/incidents/s6k0dnn5347b", incident.URL())
}

func TestNewCachedIncidentLookup(t *testing.T) {
	c := newCachedIncidentLookup(func() []Incident { return nil })
	assert.Equal(t, time.Minute, c.ttl)
	assert.Equal(t, 1, c.cache.Capacity())
}

func TestCachedIncidentLookup(t *testing.T) {
	expected := []Incident{
		Incident{
			Name: "An incident",
			ID:   "incidentID",
		},
	}

	var shouldCallUncachedLookup bool
	c := &cachedIncidentLookup{
		cache: lrucache.NewLRUCache(1),
		ttl:   50 * time.Millisecond,
		uncachedLookup: func() []Incident {
			if !shouldCallUncachedLookup {
				t.Fatal("uncachedLookup shouldn't have been called")
			}
			return expected
		},
	}

	shouldCallUncachedLookup = true
	assert.Equal(t, expected, c.ActiveIncidents())

	shouldCallUncachedLookup = false
	assert.Equal(t, expected, c.ActiveIncidents())
	assert.Equal(t, expected, c.ActiveIncidents())

	time.Sleep(50 * time.Millisecond)
	shouldCallUncachedLookup = true
	assert.Equal(t, expected, c.ActiveIncidents())
}

func TestCachedIncidentLookupDoesntPanic(t *testing.T) {
	expected := []Incident{
		Incident{
			Name: "An incident",
			ID:   "incidentID",
		},
	}
	c := &cachedIncidentLookup{
		cache:          lrucache.NewLRUCache(1),
		ttl:            50 * time.Millisecond,
		uncachedLookup: func() []Incident { return expected },
	}
	c.cache.Set(cacheKey, 42, time.Now().Add(30*time.Minute))
	actual := c.ActiveIncidents()
	assert.Equal(t, expected, actual)
}
