package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	statusURL          = "https://www.githubstatus.com/api/v2/components.json"
	unresolvedURL      = "https://www.githubstatus.com/api/v2/incidents/unresolved.json"
	statusSiteURL      = "https://www.githubstatus.com/"
	requestTimeout     = 10 * time.Second
	detailsArg         = "details"
	referenceComponent = "Visit www.githubstatus.com for more information"
)

type component struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Group  bool   `json:"group"`
}

type statusResponse struct {
	Components []component `json:"components"`
}

type incidentResponse struct {
	Incidents []incident `json:"incidents"`
}

type incident struct {
	Name            string           `json:"name"`
	Status          string           `json:"status"`
	Impact          string           `json:"impact"`
	Shortlink       string           `json:"shortlink"`
	IncidentUpdates []incidentUpdate `json:"incident_updates"`
}

type incidentUpdate struct {
	Status    string `json:"status"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

var statusIcons = map[string]string{
	"operational":           "🟢",
	"degraded_performance":  "🟡",
	"partial_outage":        "🟡",
	"major_outage":          "🔴",
	"under_maintenance":     "🟡",
	"degraded-service":      "🟡",
	"outage":                "🔴",
	"maintenance":           "🟡",
	"critical":              "🔴",
	"minor_outage":          "🟡",
	"investigating":         "🟡",
	"identified":            "🟡",
	"monitoring":            "🟡",
	"resolved":              "🟢",
	"postmortem":            "🟡",
	"maintenance_hold":      "🟡",
	"completed":             "🟢",
	"scheduled":             "🟡",
	"in_progress":           "🟡",
	"verifying":             "🟡",
	"partial_system_outage": "🟡",
}

func main() {
	showDetails, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := run(context.Background(), showDetails); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}

	if len(args) == 1 && strings.EqualFold(args[0], detailsArg) {
		return true, nil
	}

	return false, fmt.Errorf("unknown argument: %s", strings.Join(args, " "))
}

func run(ctx context.Context, showDetails bool) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	components, err := fetchComponents(ctx)
	if err != nil {
		return err
	}

	printed := printComponents(components)
	if printed == 0 {
		return errors.New("github status: no components returned")
	}

	if showDetails {
		if err := printIncidentDetails(ctx); err != nil {
			return err
		}
	}

	fmt.Printf("\nSee full incident history: %s\n", statusSiteURL)

	return nil
}

func fetchComponents(ctx context.Context) ([]component, error) {
	var payload statusResponse
	if err := fetchJSON(ctx, statusURL, &payload); err != nil {
		return nil, err
	}
	return payload.Components, nil
}

func fetchJSON(ctx context.Context, url string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github status: unexpected status %s for %s", resp.Status, url)
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response from %s: %w", url, err)
	}

	return nil
}

func printComponents(components []component) int {
	var printed int
	for _, comp := range components {
		if comp.Group || isReferenceComponent(comp.Name) {
			continue
		}
		icon := statusIcon(comp.Status)
		fmt.Printf("%s %s - %s\n", icon, comp.Name, formatStatus(comp.Status))
		printed++
	}
	return printed
}

func printIncidentDetails(ctx context.Context) error {
	var payload incidentResponse
	if err := fetchJSON(ctx, unresolvedURL, &payload); err != nil {
		return err
	}

	if len(payload.Incidents) == 0 {
		fmt.Println("\nNo active incidents at this time.")
		return nil
	}

	fmt.Println("\nActive incidents:")
	for _, inc := range payload.Incidents {
		fmt.Printf("%s %s [%s] - %s\n", statusIcon(inc.Status), inc.Name, formatStatus(inc.Impact), formatStatus(inc.Status))

		if inc.Shortlink != "" {
			fmt.Printf("  More info: %s\n", inc.Shortlink)
		}

		if len(inc.IncidentUpdates) > 0 {
			update := latestUpdate(inc.IncidentUpdates)
			fmt.Printf("  Latest update (%s): %s\n", formatTimestamp(update.CreatedAt), strings.TrimSpace(update.Body))
		}

		fmt.Println()
	}

	return nil
}

func latestUpdate(updates []incidentUpdate) incidentUpdate {
	if len(updates) == 0 {
		return incidentUpdate{}
	}

	latest := updates[0]
	latestTime, ok := parseTime(updates[0].CreatedAt)
	if !ok {
		latestTime = time.Time{}
	}

	for _, upd := range updates[1:] {
		if t, ok := parseTime(upd.CreatedAt); ok && t.After(latestTime) {
			latest = upd
			latestTime = t
		}
	}
	return latest
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

func formatTimestamp(raw string) string {
	if t, ok := parseTime(raw); ok {
		return t.Local().Format("Jan 02 15:04")
	}
	return raw
}

func statusIcon(status string) string {
	if icon, ok := statusIcons[strings.ToLower(status)]; ok {
		return icon
	}
	return "⚪️"
}

func formatStatus(status string) string {
	parts := strings.Split(strings.ToLower(status), "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func isReferenceComponent(name string) bool {
	return strings.EqualFold(name, referenceComponent)
}
