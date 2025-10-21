# gh-down

`gh-down` is a GitHub CLI extension that prints the status of key GitHub services so you can spot outages without leaving your terminal.

## Usage

```bash
gh down
```

Example output:

```text
GitHub Service Status - Oct 21 14:32 (local time)

🟢 API Requests - Operational
🟢 Git Operations - Operational
🟡 Codespaces - Degraded Performance

Active incidents:
🟡 Codespaces Latency
  Impact: Minor
  Status: Investigating
  More info: https://www.githubstatus.com/incidents/example
  - [Oct 21 14:10] Investigating: Engineers are looking into latency

See full incident history: https://www.githubstatus.com/
```

Add flags as needed:

- `--details` to show active incidents.
- `--resolved` to see incidents resolved in the past 7 days.
- `--json` for machine-readable output.

## Installation

```bash
gh extension install Houstonwp/gh-down
```

Run the command with `gh down` whenever you want a quick health check.
