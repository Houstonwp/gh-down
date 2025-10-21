package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFormatStatus(t *testing.T) {
	cases := map[string]string{
		"major_outage":         "Major Outage",
		"degraded-performance": "Degraded Performance",
		"partial system":       "Partial System",
		"":                     "",
	}

	for input, want := range cases {
		if got := formatStatus(input); got != want {
			t.Fatalf("formatStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestStatusIcon(t *testing.T) {
	if icon := statusIcon("major_outage"); icon != "🔴" {
		t.Fatalf("statusIcon(major_outage) = %q", icon)
	}
	if icon := statusIcon("operational"); icon != "🟢" {
		t.Fatalf("statusIcon(operational) = %q", icon)
	}
	if icon := statusIcon("investigating"); icon != "🟡" {
		t.Fatalf("statusIcon(investigating) = %q", icon)
	}
	if icon := statusIcon(""); icon != "⚪️" {
		t.Fatalf("statusIcon(empty) = %q", icon)
	}
}

func TestParseFlags(t *testing.T) {
	cfg, err := parseFlags([]string{"--details", "--timeout", "15s", "--json"})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if !cfg.showDetails || cfg.output != outputJSON || cfg.timeout != 15*time.Second {
		t.Fatalf("unexpected config: %#v", cfg)
	}

	_, err = parseFlags([]string{"--timeout", "0s"})
	if err == nil {
		t.Fatal("expected error for zero timeout")
	}

	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got %v", err)
	}

	_, err = parseFlags([]string{"--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got %v", err)
	}
}

func TestRenderText(t *testing.T) {
	buf := &bytes.Buffer{}

	rep := report{
		Components: []component{
			{Name: "API Requests", Status: "operational"},
			{Name: "Codespaces", Status: "major_outage"},
		},
		Active: []incident{
			{
				Name:   "Codespaces degraded",
				Status: "investigating",
				Impact: "major",
				IncidentUpdates: []incidentUpdate{
					{Status: "investigating", Body: "Looking into it.", CreatedAt: time.Now().Add(-10 * time.Minute).Format(time.RFC3339)},
				},
			},
		},
	}

	renderText(buf, rep, config{showDetails: true, showResolved: false})

	out := buf.String()
	if !strings.Contains(out, "GitHub Service Status - ") {
		t.Fatalf("expected header, got:\n%s", out)
	}
	if !strings.Contains(out, "🟢 API Requests - Operational") {
		t.Fatalf("missing component line:\n%s", out)
	}
	if !strings.Contains(out, "Active incidents:") || !strings.Contains(out, "Codespaces degraded") {
		t.Fatalf("missing incidents section:\n%s", out)
	}
}

func TestRenderJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := report{
		Components: []component{{Name: "API", Status: "operational"}},
		Active: []incident{
			{
				Name:      "API latency",
				Status:    "investigating",
				Impact:    "minor",
				Shortlink: "https://status.example/incident",
				IncidentUpdates: []incidentUpdate{
					{Status: "investigating", Body: "Working on it", CreatedAt: time.Now().Format(time.RFC3339)},
				},
			},
		},
	}

	if err := renderJSON(buf, rep); err != nil {
		t.Fatalf("renderJSON returned error: %v", err)
	}

	var payload jsonReport
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("cannot unmarshal JSON: %v\n%s", err, buf.String())
	}

	if len(payload.Components) != 1 || payload.Components[0].Name != "API" {
		t.Fatalf("unexpected components: %#v", payload.Components)
	}
	if len(payload.ActiveIncidents) != 1 {
		t.Fatalf("unexpected active incidents: %#v", payload.ActiveIncidents)
	}
}

func TestBuildReport(t *testing.T) {
	server := newStatusServer()
	defer server.Close()

	client := newStatusClient(5 * time.Second)
	client.http = server.Client()
	client.componentsURL = server.URL + "/components.json"
	client.unresolvedURL = server.URL + "/incidents/unresolved.json"
	client.incidentsURL = server.URL + "/incidents.json"

	cfg := config{
		showDetails:  true,
		showResolved: true,
		output:       outputText,
		timeout:      5 * time.Second,
	}

	rep, err := buildReport(context.Background(), client, cfg)
	if err != nil {
		t.Fatalf("buildReport returned error: %v", err)
	}

	if len(rep.Components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(rep.Components))
	}
	if len(rep.Active) != 1 {
		t.Fatalf("expected 1 active incident, got %d", len(rep.Active))
	}
	if len(rep.Resolved) != 1 {
		t.Fatalf("expected 1 recent resolved incident, got %d", len(rep.Resolved))
	}
	if rep.Resolved[0].Name != "Recent Incident" {
		t.Fatalf("unexpected resolved incident: %#v", rep.Resolved[0])
	}
}

func newStatusServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/components.json", func(w http.ResponseWriter, r *http.Request) {
		payload := statusResponse{
			Components: []component{
				{Name: "API Requests", Status: "operational", Group: false},
				{Name: "Codespaces", Status: "major_outage", Group: false},
				{Name: referenceComponent, Status: "operational", Group: false},
				{Name: "Group Container", Status: "operational", Group: true},
			},
		}
		json.NewEncoder(w).Encode(payload)
	})

	now := time.Now().UTC()
	recent := now.Add(-24 * time.Hour)
	old := now.Add(-10 * 24 * time.Hour)

	mux.HandleFunc("/incidents/unresolved.json", func(w http.ResponseWriter, r *http.Request) {
		payload := incidentResponse{
			Incidents: []incident{
				{
					ID:        "active-1",
					Name:      "Active Incident",
					Status:    "investigating",
					Impact:    "major",
					UpdatedAt: recent.Format(time.RFC3339),
					IncidentUpdates: []incidentUpdate{
						{Status: "investigating", Body: "Investigating", CreatedAt: recent.Format(time.RFC3339)},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(payload)
	})

	mux.HandleFunc("/incidents.json", func(w http.ResponseWriter, r *http.Request) {
		payload := incidentResponse{
			Incidents: []incident{
				{
					ID:        "resolved-new",
					Name:      "Recent Incident",
					Status:    "resolved",
					Impact:    "major",
					UpdatedAt: recent.Format(time.RFC3339),
					IncidentUpdates: []incidentUpdate{
						{Status: "resolved", Body: "Fixed", CreatedAt: recent.Format(time.RFC3339)},
					},
				},
				{
					ID:        "resolved-old",
					Name:      "Old Incident",
					Status:    "resolved",
					Impact:    "major",
					UpdatedAt: old.Format(time.RFC3339),
					IncidentUpdates: []incidentUpdate{
						{Status: "resolved", Body: "Old fix", CreatedAt: old.Format(time.RFC3339)},
					},
				},
				{
					ID:        "monitoring",
					Name:      "Monitoring Incident",
					Status:    "monitoring",
					Impact:    "minor",
					UpdatedAt: recent.Format(time.RFC3339),
				},
			},
		}
		json.NewEncoder(w).Encode(payload)
	})

	return httptest.NewServer(mux)
}
