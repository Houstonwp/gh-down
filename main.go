package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	version              = "0.2.0"
	defaultTimeout       = 10 * time.Second
	defaultRetries       = 1
	detailsArg           = "details"
	timeoutArg           = "--timeout"
	retriesArg           = "--retries"
	jsonArg              = "--json"
	resolvedArg          = "--resolved"
	versionArg           = "--version"
	envLog               = "GH_DOWN_LOG"
	referenceComponent   = "Visit www.githubstatus.com for more information"
	outputText           = "text"
	outputJSON           = "json"
	userAgentFormat      = "gh-down/%s"
	maxIncidentUpdates   = 3
	maxResolvedIncidents = 5
	retryBackoffInitial  = 300 * time.Millisecond
	retryBackoffMax      = 2 * time.Second
)

var (
	statusURL     = "https://www.githubstatus.com/api/v2/components.json"
	unresolvedURL = "https://www.githubstatus.com/api/v2/incidents/unresolved.json"
	allIncidents  = "https://www.githubstatus.com/api/v2/incidents.json"
	statusSiteURL = "https://www.githubstatus.com/"
)

var debugLogger = log.New(io.Discard, "gh-down debug: ", log.LstdFlags)

func init() {
	if os.Getenv(envLog) != "" {
		debugLogger.SetOutput(os.Stderr)
	}
}

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

type options struct {
	showDetails  bool
	showResolved bool
	timeout      time.Duration
	retries      int
	outputFormat string
	printVersion bool
}

type report struct {
	Components        []component
	ActiveIncidents   []incident
	ResolvedIncidents []incident
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, errHelp) {
			printUsage(os.Stdout)
			return
		}
		fmt.Fprintln(os.Stderr, err)
		printUsage(os.Stderr)
		os.Exit(1)
	}

	if opts.printVersion {
		fmt.Printf("gh-down %s\n", version)
		return
	}

	if err := run(context.Background(), opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var errHelp = errors.New("help requested")

func parseArgs(args []string) (options, error) {
	opts := options{
		timeout:      defaultTimeout,
		retries:      defaultRetries,
		outputFormat: outputText,
	}

	if len(args) == 0 {
		return opts, nil
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--details" || strings.EqualFold(arg, detailsArg):
			opts.showDetails = true
		case arg == resolvedArg:
			opts.showResolved = true
		case arg == jsonArg:
			opts.outputFormat = outputJSON
		case arg == versionArg:
			opts.printVersion = true
		case arg == "-h" || arg == "--help":
			return opts, errHelp
		case arg == timeoutArg:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s requires a duration value (e.g. 15s)", timeoutArg)
			}
			i++
			if err := setTimeout(&opts, args[i]); err != nil {
				return opts, err
			}
		case strings.HasPrefix(arg, timeoutArg+"="):
			value := strings.TrimPrefix(arg, timeoutArg+"=")
			if err := setTimeout(&opts, value); err != nil {
				return opts, err
			}
		case arg == retriesArg:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s requires an integer value (e.g. 3)", retriesArg)
			}
			i++
			if err := setRetries(&opts, args[i]); err != nil {
				return opts, err
			}
		case strings.HasPrefix(arg, retriesArg+"="):
			value := strings.TrimPrefix(arg, retriesArg+"=")
			if err := setRetries(&opts, value); err != nil {
				return opts, err
			}
		default:
			return opts, fmt.Errorf("unknown argument: %s", arg)
		}
	}

	return opts, nil
}

func printUsage(out *os.File) {
	fmt.Fprintln(out, "Usage: gh down [--details] [--resolved] [--timeout <duration>] [--retries <count>] [--json] [--version]")
	fmt.Fprintln(out, "  --details            Show active incident summaries when available")
	fmt.Fprintln(out, "  --resolved           Include recently resolved incidents in the details output")
	fmt.Fprintln(out, "  --timeout <duration> Override the request timeout (e.g. 15s, 1m). Default is 10s.")
	fmt.Fprintln(out, "  --retries <count>    Retry failed requests up to <count> times with backoff. Default is 1.")
	fmt.Fprintln(out, "  --json               Emit machine-readable JSON instead of text output")
	fmt.Fprintln(out, "  --version            Print the extension version and exit")
}

func setTimeout(opts *options, raw string) error {
	dur, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid timeout %q: %w", raw, err)
	}
	if dur <= 0 {
		return fmt.Errorf("timeout must be greater than zero (got %s)", dur)
	}
	opts.timeout = dur
	return nil
}

func setRetries(opts *options, raw string) error {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("invalid retry count %q: %w", raw, err)
	}
	if value <= 0 {
		return fmt.Errorf("retries must be greater than zero (got %d)", value)
	}
	opts.retries = value
	return nil
}

func run(ctx context.Context, opts options) error {
	ctx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	client := &http.Client{
		Timeout: opts.timeout,
	}

	data := report{}

	components, err := fetchComponents(ctx, client, opts.retries)
	if err != nil {
		return classifyError(err, opts)
	}
	data.Components = filterComponents(components)
	if len(data.Components) == 0 {
		return errors.New("github status: no components returned")
	}

	needsIncidentData := opts.showDetails || opts.showResolved || opts.outputFormat == outputJSON
	if needsIncidentData {
		active, err := fetchIncidents(ctx, client, unresolvedURL, opts.retries)
		if err != nil {
			return classifyError(err, opts)
		}
		data.ActiveIncidents = sortIncidents(active)

		if opts.showResolved {
			all, err := fetchIncidents(ctx, client, allIncidents, opts.retries)
			if err != nil {
				return classifyError(err, opts)
			}
			data.ResolvedIncidents = filterResolved(sortIncidents(all))
		}
	}

	switch opts.outputFormat {
	case outputJSON:
		return renderJSON(data)
	default:
		renderText(data, opts)
	}

	return nil
}

func fetchComponents(ctx context.Context, client *http.Client, retries int) ([]component, error) {
	var payload statusResponse
	if err := fetchJSON(ctx, client, statusURL, &payload, retries); err != nil {
		return nil, err
	}
	return payload.Components, nil
}

func fetchJSON(ctx context.Context, client *http.Client, url string, target interface{}, retries int) error {
	attempts := retries
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error

	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("User-Agent", fmt.Sprintf(userAgentFormat, version))

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("fetch %s: %w", url, err)
			debugLogger.Printf("attempt %d failed: %v", attempt, lastErr)
		} else {
			func() {
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					lastErr = fmt.Errorf("github status: unexpected status %s for %s", resp.Status, url)
					debugLogger.Printf("attempt %d bad status: %v", attempt, lastErr)
					if !shouldRetryStatus(resp.StatusCode) || attempt == attempts {
						return
					}
				} else {
					if decodeErr := json.NewDecoder(resp.Body).Decode(target); decodeErr != nil {
						lastErr = fmt.Errorf("decode response from %s: %w", url, decodeErr)
						debugLogger.Printf("attempt %d decode failed: %v", attempt, lastErr)
					} else {
						lastErr = nil
					}
				}
			}()
		}

		if lastErr == nil {
			return nil
		}

		if attempt == attempts {
			break
		}

		if err := waitForRetry(ctx, attempt); err != nil {
			return lastErr
		}
	}

	return lastErr
}

func fetchIncidents(ctx context.Context, client *http.Client, url string, retries int) ([]incident, error) {
	var payload incidentResponse
	if err := fetchJSON(ctx, client, url, &payload, retries); err != nil {
		return nil, err
	}
	return payload.Incidents, nil
}

func waitForRetry(ctx context.Context, attempt int) error {
	backoff := retryBackoffInitial * time.Duration(1<<(attempt-1))
	if backoff > retryBackoffMax {
		backoff = retryBackoffMax
	}
	timer := time.NewTimer(backoff)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func shouldRetryStatus(status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	return status >= 500 && status <= 599
}

func classifyError(err error, opts options) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("request timed out after %s; try increasing --timeout or --retries", opts.timeout)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("request timed out after %s; try increasing --timeout or --retries", opts.timeout)
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Errorf("github status: invalid JSON response (offset %d)", syntaxErr.Offset)
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Errorf("github status: unexpected data shape (%s)", typeErr.Field)
	}

	return err
}

func filterComponents(components []component) []component {
	filtered := make([]component, 0, len(components))
	for _, comp := range components {
		if comp.Group || isReferenceComponent(comp.Name) {
			continue
		}
		filtered = append(filtered, comp)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
	})

	return filtered
}

func renderText(data report, opts options) {
	reportedAt := time.Now().Local()
	fmt.Printf("GitHub Service Status - %s (local time)\n\n", reportedAt.Format("Jan 02 15:04"))

	for _, comp := range data.Components {
		fmt.Printf("%s %s - %s\n", statusIcon(comp.Status), comp.Name, formatStatus(comp.Status))
	}

	if opts.showDetails {
		fmt.Println()
		renderIncidentSection("Active incidents", data.ActiveIncidents, "No active incidents at this time.")
	}

	if opts.showResolved {
		fmt.Println()
		renderIncidentSection("Recently resolved incidents", data.ResolvedIncidents, "No recently resolved incidents.")
	}

	fmt.Printf("\nSee full incident history: %s\n", statusSiteURL)
}

func renderIncidentSection(title string, incidents []incident, empty string) {
	fmt.Println(title + ":")
	if len(incidents) == 0 {
		fmt.Printf("  %s\n", empty)
		return
	}

	for _, inc := range incidents {
		fmt.Printf("%s %s\n", statusIcon(inc.Status), inc.Name)
		if impact := formatStatus(inc.Impact); impact != "" && !strings.EqualFold(impact, "None") {
			fmt.Printf("  Impact: %s\n", impact)
		}
		fmt.Printf("  Status: %s\n", formatStatus(inc.Status))
		if inc.Shortlink != "" {
			fmt.Printf("  More info: %s\n", inc.Shortlink)
		}

		for _, update := range summarizeUpdates(inc.IncidentUpdates) {
			fmt.Printf("  - [%s] %s: %s\n",
				formatTimestamp(update.CreatedAt),
				formatStatus(update.Status),
				summarizeBody(update.Body),
			)
		}

		fmt.Println()
	}
}

func summarizeBody(body string) string {
	return strings.Join(strings.Fields(body), " ")
}

func summarizeUpdates(updates []incidentUpdate) []incidentUpdate {
	sorted := sortIncidentUpdates(updates)
	if len(sorted) > maxIncidentUpdates {
		return sorted[:maxIncidentUpdates]
	}
	return sorted
}

func sortIncidentUpdates(updates []incidentUpdate) []incidentUpdate {
	sorted := make([]incidentUpdate, len(updates))
	copy(sorted, updates)
	sort.Slice(sorted, func(i, j int) bool {
		ti, _ := parseTime(sorted[i].CreatedAt)
		tj, _ := parseTime(sorted[j].CreatedAt)
		return ti.After(tj)
	})
	return sorted
}

func sortIncidents(incidents []incident) []incident {
	sorted := make([]incident, len(incidents))
	copy(sorted, incidents)

	sort.Slice(sorted, func(i, j int) bool {
		ri := impactOrder(sorted[i].Impact)
		rj := impactOrder(sorted[j].Impact)
		if ri != rj {
			return ri < rj
		}

		ti := incidentTime(sorted[i])
		tj := incidentTime(sorted[j])
		if !ti.Equal(tj) {
			return ti.After(tj)
		}

		return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
	})

	return sorted
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
	for _, update := range sortIncidentUpdates(inc.IncidentUpdates) {
		if t, ok := parseTime(update.CreatedAt); ok {
			return t
		}
	}
	return time.Time{}
}

func filterResolved(incidents []incident) []incident {
	resolved := make([]incident, 0, len(incidents))
	seen := make(map[string]struct{})

	for _, inc := range incidents {
		if !strings.EqualFold(inc.Status, "resolved") {
			continue
		}
		if inc.ID != "" {
			if _, ok := seen[inc.ID]; ok {
				continue
			}
			seen[inc.ID] = struct{}{}
		}
		resolved = append(resolved, inc)
		if len(resolved) >= maxResolvedIncidents {
			break
		}
	}

	return resolved
}

func renderJSON(data report) error {
	payload := struct {
		Components        []componentReport `json:"components"`
		ActiveIncidents   []incidentReport  `json:"active_incidents,omitempty"`
		ResolvedIncidents []incidentReport  `json:"resolved_incidents,omitempty"`
		StatusPage        string            `json:"status_page"`
		GeneratedAt       string            `json:"generated_at"`
	}{
		Components:  make([]componentReport, 0, len(data.Components)),
		StatusPage:  statusSiteURL,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	for _, comp := range data.Components {
		payload.Components = append(payload.Components, componentReport{
			Name:       comp.Name,
			Status:     strings.ToLower(strings.TrimSpace(comp.Status)),
			StatusText: formatStatus(comp.Status),
			Icon:       statusIcon(comp.Status),
		})
	}

	for _, inc := range data.ActiveIncidents {
		payload.ActiveIncidents = append(payload.ActiveIncidents, buildIncidentReport(inc))
	}

	for _, inc := range data.ResolvedIncidents {
		payload.ResolvedIncidents = append(payload.ResolvedIncidents, buildIncidentReport(inc))
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

type componentReport struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	StatusText string `json:"status_text"`
	Icon       string `json:"icon"`
}

type incidentReport struct {
	Name         string                 `json:"name"`
	Impact       string                 `json:"impact"`
	Status       string                 `json:"status"`
	StatusText   string                 `json:"status_text"`
	Shortlink    string                 `json:"shortlink,omitempty"`
	UpdatedAt    string                 `json:"updated_at,omitempty"`
	LatestUpdate *incidentUpdateReport  `json:"latest_update,omitempty"`
	Updates      []incidentUpdateReport `json:"updates,omitempty"`
}

type incidentUpdateReport struct {
	Status     string `json:"status"`
	StatusText string `json:"status_text"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
}

func buildIncidentReport(inc incident) incidentReport {
	sortedUpdates := summarizeUpdates(inc.IncidentUpdates)
	report := incidentReport{
		Name:       inc.Name,
		Impact:     strings.ToLower(strings.TrimSpace(inc.Impact)),
		Status:     strings.ToLower(strings.TrimSpace(inc.Status)),
		StatusText: formatStatus(inc.Status),
		Shortlink:  inc.Shortlink,
		UpdatedAt:  inc.UpdatedAt,
	}

	if report.UpdatedAt == "" {
		if t := incidentTime(inc); !t.IsZero() {
			report.UpdatedAt = t.Format(time.RFC3339)
		}
	}

	for i, upd := range sortedUpdates {
		item := incidentUpdateReport{
			Status:     strings.ToLower(strings.TrimSpace(upd.Status)),
			StatusText: formatStatus(upd.Status),
			Body:       summarizeBody(upd.Body),
			CreatedAt:  upd.CreatedAt,
		}
		report.Updates = append(report.Updates, item)
		if i == 0 {
			report.LatestUpdate = &report.Updates[len(report.Updates)-1]
		}
	}

	return report
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
	switch s := strings.ToLower(strings.TrimSpace(status)); s {
	case "":
		return "⚪️"
	case "operational", "resolved", "completed":
		return "🟢"
	case "major_outage", "outage", "critical":
		return "🔴"
	default:
		return "🟡"
	}
}

func formatStatus(status string) string {
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

func isReferenceComponent(name string) bool {
	return strings.EqualFold(name, referenceComponent)
}
