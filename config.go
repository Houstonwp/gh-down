package main

import (
	"flag"
	"fmt"
	"time"
)

const (
	version            = "0.3.0"
	defaultTimeout     = 10 * time.Second
	statusSiteURL      = "https://www.githubstatus.com/"
	outputText         = "text"
	outputJSON         = "json"
	referenceComponent = "Visit www.githubstatus.com for more information"
	resolvedLookback   = 7 * 24 * time.Hour
)

type config struct {
	showDetails  bool
	showResolved bool
	showVersion  bool
	output       string
	timeout      time.Duration
}

func parseFlags(args []string) (config, error) {
	cfg := config{
		timeout: defaultTimeout,
		output:  outputText,
	}

	fs := flag.NewFlagSet("gh-down", flag.ContinueOnError)

	fs.BoolVar(&cfg.showDetails, "details", false, "Show active incidents when available")
	fs.BoolVar(&cfg.showResolved, "resolved", false, "Include recently resolved incidents (last 7 days)")
	fs.BoolVar(&cfg.showVersion, "version", false, "Print version and exit")
	fs.DurationVar(&cfg.timeout, "timeout", defaultTimeout, "Override network timeout (e.g. 15s, 1m)")

	jsonOutput := fs.Bool("json", false, "Emit machine-readable JSON")

	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gh down [options]")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	if cfg.timeout <= 0 {
		return cfg, fmt.Errorf("timeout must be greater than zero")
	}

	if *jsonOutput {
		cfg.output = outputJSON
	}

	return cfg, nil
}
