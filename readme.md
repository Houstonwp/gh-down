# gh-down

`gh-down` is a GitHub CLI extension that reports the current status of core GitHub services. It lets you quickly confirm whether GitHub is experiencing disruptions without leaving your terminal.

## Usage

```bash
gh down
```

The command queries the GitHub status API and prints a summary of each monitored component, making it easy to spot degraded performance or outages.

## Installation

```bash
gh extension install <local-path-or-repo>
```

Install the extension from this repository or your fork, then run `gh down` to check service health whenever you need a quick status update.

## Development

The project includes a GitHub Actions workflow that runs `gofmt`, `go vet`, and `go test ./...` on each push and pull request. Run the same commands locally before opening a PR to catch issues early.
