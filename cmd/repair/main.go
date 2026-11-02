// repair requests model-generated changes and verifies them in Docker.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/akshay-dhangar/issuepilot/internal/agent"
	"github.com/akshay-dhangar/issuepilot/internal/model"
	"github.com/akshay-dhangar/issuepilot/internal/sandbox"
)

func main() { os.Exit(run()) }
func run() int {
	repo := flag.String("repo", "", "local repository; root Go source will be sent to OpenAI")
	commit := flag.String("commit", "", "exact 40-character commit SHA")
	issuePath := flag.String("issue-file", "", "UTF-8 issue text file (max 8 KiB)")
	name := flag.String("model", "", "explicit OpenAI model with Responses structured-output support")
	output := flag.String("output", "", "NEW directory for report.json and successful patch.diff")
	attempts := flag.Int("attempts", 2, "maximum repair proposals (1 or 2)")
	flag.Parse()
	if *repo == "" || *commit == "" || *issuePath == "" || *output == "" || *name == "" || flag.NArg() != 0 {
		flag.Usage()
		return 2
	}
	if *attempts < 1 || *attempts > 2 {
		fmt.Fprintln(os.Stderr, "attempts must be 1 or 2")
		return 2
	}
	provider, err := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"), *name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	file, err := os.Open(*issuePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	issue, err := io.ReadAll(io.LimitReader(file, 8193))
	_ = file.Close()
	if err != nil || len(issue) > 8192 {
		fmt.Fprintln(os.Stderr, "issue could not be read or exceeds 8 KiB")
		return 2
	}
	// Refuse existing directories so evidence from an earlier run is never overwritten.
	if err = os.Mkdir(*output, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "create new evidence directory:", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	engine := agent.Engine{Provider: provider, Tester: sandbox.Docker{Image: sandbox.DefaultImage, Timeout: 2 * time.Minute}, MaxAttempts: *attempts}
	report, runErr := engine.Run(ctx, *repo, *commit, string(issue))
	data, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(*output, "report.json"), append(data, '\n'), 0600)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "save report:", err)
		return 2
	}
	if report.Patch != "" {
		if err = os.WriteFile(filepath.Join(*output, "patch.diff"), []byte(report.Patch), 0600); err != nil {
			fmt.Fprintln(os.Stderr, "save patch:", err)
			return 2
		}
	}
	fmt.Printf("%s: %s\n", report.Status, filepath.Join(*output, "report.json"))
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		return 1
	}
	return 0
}
