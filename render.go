package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
)

const maxIncidentUpdates = 3

func renderReport(r report, cfg config) error {
	switch cfg.output {
	case outputJSON:
		return renderJSON(os.Stdout, r)
	default:
		renderText(os.Stdout, r, cfg)
		return nil
	}
}

func renderText(w io.Writer, r report, cfg config) {
	fmt.Fprintf(w, "GitHub Service Status - %s (local time)\n\n", time.Now().Local().Format("Jan 02 15:04"))

	for _, comp := range r.Components {
		fmt.Fprintf(w, "%s %s - %s\n", statusIcon(comp.Status), comp.Name, formatStatus(comp.Status))
	}

	if cfg.showDetails {
		fmt.Fprintln(w)
		printIncidentSection(w, "Active incidents", r.Active, "No active incidents at this time.")
	}

	if cfg.showResolved {
		fmt.Fprintln(w)
		printIncidentSection(w, "Recently resolved incidents", r.Resolved, "No recently resolved incidents in the last 7 days.")
	}

	fmt.Fprintf(w, "\nSee full incident history: %s\n", statusSiteURL)
}

func printIncidentSection(w io.Writer, title string, incidents []incident, emptyMessage string) {
	fmt.Fprintln(w, title+":")
	if len(incidents) == 0 {
		fmt.Fprintf(w, "  %s\n", emptyMessage)
		return
	}

	for _, inc := range incidents {
		fmt.Fprintf(w, "%s %s\n", statusIcon(inc.Status), inc.Name)
		if impact := formatStatus(inc.Impact); impact != "" && !strings.EqualFold(impact, "None") {
			fmt.Fprintf(w, "  Impact: %s\n", impact)
		}
		fmt.Fprintf(w, "  Status: %s\n", formatStatus(inc.Status))
		if inc.Shortlink != "" {
			fmt.Fprintf(w, "  More info: %s\n", inc.Shortlink)
		}

		for _, update := range summarizeUpdates(inc.IncidentUpdates) {
			fmt.Fprintf(w, "  - [%s] %s: %s\n",
				formatTimestamp(update.CreatedAt),
				formatStatus(update.Status),
				summarizeBody(update.Body),
			)
		}

		fmt.Fprintln(w)
	}
}

func summarizeUpdates(updates []incidentUpdate) []incidentUpdate {
	if len(updates) <= maxIncidentUpdates {
		return updates
	}
	return updates[:maxIncidentUpdates]
}

func renderJSON(w io.Writer, r report) error {
	payload := jsonReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		StatusPage:  statusSiteURL,
		Components:  make([]jsonComponent, 0, len(r.Components)),
	}

	for _, comp := range r.Components {
		payload.Components = append(payload.Components, jsonComponent{
			Name:       comp.Name,
			Status:     strings.ToLower(strings.TrimSpace(comp.Status)),
			StatusText: formatStatus(comp.Status),
			Icon:       statusIcon(comp.Status),
		})
	}

	if len(r.Active) > 0 {
		payload.ActiveIncidents = make([]jsonIncident, 0, len(r.Active))
		for _, inc := range r.Active {
			payload.ActiveIncidents = append(payload.ActiveIncidents, buildJSONIncident(inc))
		}
	}

	if len(r.Resolved) > 0 {
		payload.ResolvedIncidents = make([]jsonIncident, 0, len(r.Resolved))
		for _, inc := range r.Resolved {
			payload.ResolvedIncidents = append(payload.ResolvedIncidents, buildJSONIncident(inc))
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

type jsonReport struct {
	GeneratedAt       string          `json:"generated_at"`
	StatusPage        string          `json:"status_page"`
	Components        []jsonComponent `json:"components"`
	ActiveIncidents   []jsonIncident  `json:"active_incidents,omitempty"`
	ResolvedIncidents []jsonIncident  `json:"resolved_incidents,omitempty"`
}

type jsonComponent struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	StatusText string `json:"status_text"`
	Icon       string `json:"icon"`
}

type jsonIncident struct {
	Name       string               `json:"name"`
	Impact     string               `json:"impact"`
	Status     string               `json:"status"`
	StatusText string               `json:"status_text"`
	Shortlink  string               `json:"shortlink,omitempty"`
	UpdatedAt  string               `json:"updated_at,omitempty"`
	Updates    []jsonIncidentUpdate `json:"updates,omitempty"`
}

type jsonIncidentUpdate struct {
	Status     string `json:"status"`
	StatusText string `json:"status_text"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
}

func buildJSONIncident(inc incident) jsonIncident {
	result := jsonIncident{
		Name:       inc.Name,
		Impact:     strings.ToLower(strings.TrimSpace(inc.Impact)),
		Status:     strings.ToLower(strings.TrimSpace(inc.Status)),
		StatusText: formatStatus(inc.Status),
		Shortlink:  inc.Shortlink,
		UpdatedAt:  inc.UpdatedAt,
	}

	if result.UpdatedAt == "" {
		if t := incidentTime(inc); !t.IsZero() {
			result.UpdatedAt = t.Format(time.RFC3339)
		}
	}

	for _, update := range summarizeUpdates(inc.IncidentUpdates) {
		result.Updates = append(result.Updates, jsonIncidentUpdate{
			Status:     strings.ToLower(strings.TrimSpace(update.Status)),
			StatusText: formatStatus(update.Status),
			Body:       summarizeBody(update.Body),
			CreatedAt:  update.CreatedAt,
		})
	}

	return result
}

func statusIcon(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return "⚪️"
	case "operational", "resolved", "completed":
		return "🟢"
	case "major_outage", "critical", "outage":
		return "🔴"
	default:
		return "🟡"
	}
}

func formatStatus(status string) string {
	if status == "" {
		return ""
	}
	fields := strings.FieldsFunc(status, func(r rune) bool {
		return r == '_' || r == '-' || unicode.IsSpace(r)
	})
	for i, field := range fields {
		if field == "" {
			continue
		}
		lower := strings.ToLower(field)
		fields[i] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	return strings.Join(fields, " ")
}
