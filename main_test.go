package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFormatStatus(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"major_outage":          "Major Outage",
		"degraded-performance":  "Degraded Performance",
		"partial system outage": "Partial System Outage",
		"":                      "",
	}

	for input, want := range cases {
		if got := formatStatus(input); got != want {
			t.Fatalf("formatStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestStatusIcon(t *testing.T) {
	t.Parallel()

	if got := statusIcon("major_outage"); got != "🔴" {
		t.Fatalf("statusIcon(major_outage) = %q, want 🔴", got)
	}
	if got := statusIcon("operational"); got != "🟢" {
		t.Fatalf("statusIcon(operational) = %q, want 🟢", got)
	}
	if got := statusIcon("investigating"); got != "🟡" {
		t.Fatalf("statusIcon(investigating) = %q, want 🟡", got)
	}
}

func TestParseArgs(t *testing.T) {
	t.Parallel()

	opts, err := parseArgs([]string{})
	if err != nil {
		t.Fatalf("parseArgs([]) error = %v", err)
	}
	if opts.timeout != defaultTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultTimeout, opts.timeout)
	}
	if opts.outputFormat != outputText {
		t.Fatalf("expected default output format %s, got %s", outputText, opts.outputFormat)
	}

	args := []string{"details", "--timeout", "20s", "--retries", "3", "--json"}
	opts, err = parseArgs(args)
	if err != nil {
		t.Fatalf("parseArgs(%v) error = %v", args, err)
	}
	if !opts.showDetails {
		t.Fatal("expected showDetails to be true")
	}
	if opts.timeout != 20*time.Second {
		t.Fatalf("expected timeout 20s, got %s", opts.timeout)
	}
	if opts.retries != 3 {
		t.Fatalf("expected retries 3, got %d", opts.retries)
	}
	if opts.outputFormat != outputJSON {
		t.Fatalf("expected output format json, got %s", opts.outputFormat)
	}

	opts, err = parseArgs([]string{"--version"})
	if err != nil {
		t.Fatalf("parseArgs(--version) error = %v", err)
	}
	if !opts.printVersion {
		t.Fatal("expected printVersion to be true")
	}

	if _, err := parseArgs([]string{"--timeout"}); err == nil {
		t.Fatal("expected error for missing timeout value")
	}
}

func TestRunTextOutput(t *testing.T) {
	server := newStatusServer()
	defer server.Close()

	restoreEndpoints := hijackEndpoints(server.URL)
	defer restoreEndpoints()

	out, err := captureOutput(func() error {
		return run(context.Background(), options{
			showDetails:  true,
			showResolved: true,
			timeout:      5 * time.Second,
			retries:      2,
			outputFormat: outputText,
			printVersion: false,
		})
	})
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if !containsAll(out,
		"GitHub Service Status - ",
		"Codespaces - Major Outage",
		"Git Operations - Operational",
		"Active incidents:",
		"Recently resolved incidents:",
		"Identified: Issue identified",
		"More info: https://status.example/active",
		"More info: https://status.example/resolved") {
		t.Fatalf("unexpected text output:\n%s", out)
	}
}

func TestRunJSONOutput(t *testing.T) {
	server := newStatusServer()
	defer server.Close()

	restoreEndpoints := hijackEndpoints(server.URL)
	defer restoreEndpoints()

	out, err := captureOutput(func() error {
		return run(context.Background(), options{
			showDetails:  false,
			showResolved: true,
			timeout:      5 * time.Second,
			retries:      2,
			outputFormat: outputJSON,
			printVersion: false,
		})
	})
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	var payload struct {
		Components        []componentReport `json:"components"`
		ActiveIncidents   []incidentReport  `json:"active_incidents"`
		ResolvedIncidents []incidentReport  `json:"resolved_incidents"`
		StatusPage        string            `json:"status_page"`
		GeneratedAt       string            `json:"generated_at"`
	}

	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal output: %v\noutput was:\n%s", err, out)
	}

	if len(payload.Components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(payload.Components))
	}
	if payload.Components[0].Name != "Codespaces" || payload.Components[0].Icon != "🔴" {
		t.Fatalf("unexpected first component: %#v", payload.Components[0])
	}
	if payload.StatusPage != statusSiteURL {
		t.Fatalf("unexpected status page: %s", payload.StatusPage)
	}
	if payload.GeneratedAt == "" {
		t.Fatal("expected generated_at to be populated")
	}
	if len(payload.ActiveIncidents) == 0 || payload.ActiveIncidents[0].LatestUpdate == nil {
		t.Fatalf("expected active incidents with latest update: %#v", payload.ActiveIncidents)
	}
	if len(payload.ResolvedIncidents) == 0 {
		t.Fatal("expected resolved incidents in JSON output")
	}
}

func captureOutput(fn func() error) (string, error) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}

	os.Stdout = w
	runErr := fn()
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return "", err
	}
	r.Close()
	return buf.String(), runErr
}

func containsAll(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func hijackEndpoints(base string) func() {
	prevStatusURL := statusURL
	prevUnresolved := unresolvedURL
	prevAll := allIncidents
	prevSite := statusSiteURL

	statusURL = base + "/components.json"
	unresolvedURL = base + "/incidents/unresolved.json"
	allIncidents = base + "/incidents.json"
	statusSiteURL = base + "/status"

	return func() {
		statusURL = prevStatusURL
		unresolvedURL = prevUnresolved
		allIncidents = prevAll
		statusSiteURL = prevSite
	}
}

func newStatusServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/components.json", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{
			"components": [
				{"name":"Git Operations","status":"operational","group":false},
				{"name":"Codespaces","status":"major_outage","group":false},
				{"name":"Visit www.githubstatus.com for more information","status":"operational","group":false},
				{"name":"Groups","status":"operational","group":true}
			]
		}`)
	})

	mux.HandleFunc("/incidents/unresolved.json", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{
			"incidents": [
				{
					"id":"active-1",
					"name":"Codespaces Degraded",
					"status":"investigating",
					"impact":"major",
					"shortlink":"https://status.example/active",
					"created_at":"2024-01-01T00:00:00Z",
					"updated_at":"2024-01-01T01:00:00Z",
					"incident_updates":[
						{"status":"investigating","body":"We are looking into it.","created_at":"2024-01-01T00:05:00Z"},
						{"status":"identified","body":"Issue identified","created_at":"2024-01-01T00:30:00Z"},
						{"status":"monitoring","body":"Monitoring after mitigation.","created_at":"2024-01-01T01:15:00Z"}
					]
				}
			]
		}`)
	})

	mux.HandleFunc("/incidents.json", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{
			"incidents": [
				{
					"id":"resolved-1",
					"name":"API Incident",
					"status":"resolved",
					"impact":"major",
					"shortlink":"https://status.example/resolved",
					"created_at":"2023-12-31T00:00:00Z",
					"updated_at":"2024-01-02T03:00:00Z",
					"incident_updates":[
						{"status":"resolved","body":"Issue resolved.","created_at":"2024-01-02T03:00:00Z"}
					]
				},
				{
					"id":"resolved-2",
					"name":"Dependabot Delay",
					"status":"resolved",
					"impact":"minor",
					"shortlink":"https://status.example/resolved2",
					"created_at":"2023-12-31T00:00:00Z",
					"updated_at":"2024-01-01T01:00:00Z",
					"incident_updates":[
						{"status":"resolved","body":"Delays cleared.","created_at":"2024-01-01T01:00:00Z"}
					]
				},
				{
					"id":"monitoring-1",
					"name":"Ongoing Incident",
					"status":"monitoring",
					"impact":"minor",
					"shortlink":"https://status.example/monitoring",
					"created_at":"2024-01-02T00:00:00Z",
					"updated_at":"2024-01-02T02:00:00Z",
					"incident_updates":[
						{"status":"monitoring","body":"Monitoring ongoing.","created_at":"2024-01-02T02:00:00Z"}
					]
				}
			]
		}`)
	})

	return httptest.NewServer(mux)
}
