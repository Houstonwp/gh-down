# gh-down

`gh-down` is a GitHub CLI extension that prints the status of key GitHub services so you can spot outages without leaving your terminal.

## Usage

```bash
gh down
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
