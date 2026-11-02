// issuepilot runs tests for a pinned local Go repository in a restricted container.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/akshay-dhangar/issuepilot/internal/sandbox"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() { os.Exit(run()) }
func run() int {
	repo := flag.String("repo", "", "local Git repository to snapshot")
	commit := flag.String("commit", "", "full 40-character commit SHA (required)")
	timeout := flag.Duration("timeout", 2*time.Minute, "test timeout, up to 5m")
	flag.Parse()
	if *repo == "" || *commit == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: issuepilot -repo PATH -commit FULL_SHA [-timeout 2m]")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	prepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	snapshot, err := sandbox.Prepare(prepCtx, *repo, *commit)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer snapshot.Close()
	result, err := (sandbox.Docker{Image: sandbox.DefaultImage, Timeout: *timeout}).Test(ctx, snapshot)
	if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
		fmt.Fprintln(os.Stderr, encodeErr)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if result.ExitCode != 0 {
		return 1
	}
	return 0
}
