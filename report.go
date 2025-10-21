package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type report struct {
	Components []component
	Active     []incident
	Resolved   []incident
}

func buildReport(ctx context.Context, client *statusClient, cfg config) (report, error) {
	comps, err := client.Components(ctx)
	if err != nil {
		return report{}, err
	}

	r := report{
		Components: filterComponents(comps),
	}

	if len(r.Components) == 0 {
		return report{}, fmt.Errorf("github status returned no components")
	}

	includeActive := cfg.showDetails || cfg.output == outputJSON
	includeResolved := cfg.showResolved || cfg.output == outputJSON

	if includeActive {
		active, err := client.ActiveIncidents(ctx)
		if err != nil {
			return report{}, err
		}
		r.Active = sortIncidents(active)
	}

	if includeResolved {
		resolved, err := client.RecentResolvedIncidents(ctx, resolvedLookback)
		if err != nil {
			return report{}, err
		}
		r.Resolved = sortIncidents(resolved)
	}

	return r, nil
}

func filterComponents(components []component) []component {
	out := make([]component, 0, len(components))
	for _, comp := range components {
		if comp.Group {
			continue
		}
		if strings.EqualFold(comp.Name, referenceComponent) {
			continue
		}
		out = append(out, comp)
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})

	return out
}

func sortIncidents(incidents []incident) []incident {
	out := make([]incident, len(incidents))
	copy(out, incidents)

	sort.Slice(out, func(i, j int) bool {
		ti := incidentTime(out[i])
		tj := incidentTime(out[j])
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		impactCompare := impactOrder(out[i].Impact) - impactOrder(out[j].Impact)
		if impactCompare != 0 {
			return impactCompare < 0
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})

	return out
}

func impactOrder(impact string) int {
	switch strings.ToLower(strings.TrimSpace(impact)) {
	case "critical":
		return 0
	case "major":
		return 1
	case "minor":
		return 2
	case "none":
		return 3
	default:
		return 4
	}
}

func formatTimestamp(raw string) string {
	if t, ok := parseTime(raw); ok {
		return t.Local().Format("Jan 02 15:04")
	}
	return raw
}

func summarizeBody(body string) string {
	return strings.Join(strings.Fields(body), " ")
}
