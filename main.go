package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
)

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if cfg.showVersion {
		fmt.Printf("gh-down %s\n", version)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	client := newStatusClient(cfg.timeout)

	rep, err := buildReport(ctx, client, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := renderReport(rep, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
