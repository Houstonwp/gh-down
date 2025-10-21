package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	componentsURL = "https://www.githubstatus.com/api/v2/components.json"
	unresolvedURL = "https://www.githubstatus.com/api/v2/incidents/unresolved.json"
	incidentsURL  = "https://www.githubstatus.com/api/v2/incidents.json"
	userAgent     = "gh-down/" + version
)

type statusClient struct {
	http          *http.Client
	componentsURL string
	unresolvedURL string
	incidentsURL  string
}

func newStatusClient(timeout time.Duration) *statusClient {
	return &statusClient{
		http: &http.Client{
			Timeout: timeout,
		},
		componentsURL: componentsURL,
		unresolvedURL: unresolvedURL,
		incidentsURL:  incidentsURL,
	}
}

func (c *statusClient) Components(ctx context.Context) ([]component, error) {
	var payload statusResponse
	if err := c.get(ctx, c.componentsURL, &payload); err != nil {
		return nil, fmt.Errorf("fetch components: %w", err)
	}
	return payload.Components, nil
}

func (c *statusClient) ActiveIncidents(ctx context.Context) ([]incident, error) {
	var payload incidentResponse
	if err := c.get(ctx, c.unresolvedURL, &payload); err != nil {
		return nil, fmt.Errorf("fetch active incidents: %w", err)
	}
	return payload.Incidents, nil
}

func (c *statusClient) RecentResolvedIncidents(ctx context.Context, lookback time.Duration) ([]incident, error) {
	var payload incidentResponse
	if err := c.get(ctx, c.incidentsURL, &payload); err != nil {
		return nil, fmt.Errorf("fetch resolved incidents: %w", err)
	}

	cutoff := time.Now().Add(-lookback)
	results := make([]incident, 0, len(payload.Incidents))
	seen := make(map[string]struct{})

	for _, inc := range payload.Incidents {
		if !strings.EqualFold(inc.Status, "resolved") {
			continue
		}

		if inc.ID != "" {
			if _, found := seen[inc.ID]; found {
				continue
			}
			seen[inc.ID] = struct{}{}
		}

		if t := incidentTime(inc); !t.IsZero() && t.Before(cutoff) {
			continue
		}

		results = append(results, inc)
	}

	return results, nil
}

func (c *statusClient) get(ctx context.Context, url string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response: %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

type statusResponse struct {
	Components []component `json:"components"`
}

type incidentResponse struct {
	Incidents []incident `json:"incidents"`
}

type component struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Group  bool   `json:"group"`
}

type incident struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Status          string           `json:"status"`
	Impact          string           `json:"impact"`
	Shortlink       string           `json:"shortlink"`
	CreatedAt       string           `json:"created_at"`
	UpdatedAt       string           `json:"updated_at"`
	IncidentUpdates []incidentUpdate `json:"incident_updates"`
}

type incidentUpdate struct {
	Status    string `json:"status"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

func incidentTime(inc incident) time.Time {
	if t, ok := parseTime(inc.UpdatedAt); ok {
		return t
	}
	if len(inc.IncidentUpdates) == 0 {
		if t, ok := parseTime(inc.CreatedAt); ok {
			return t
		}
		return time.Time{}
	}
	for _, upd := range inc.IncidentUpdates {
		if t, ok := parseTime(upd.CreatedAt); ok {
			return t
		}
	}
	return time.Time{}
}

func parseTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}
