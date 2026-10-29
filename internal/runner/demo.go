// Package runner executes only source fixtures compiled into this program.
// It is NOT a sandbox for untrusted repository code.
package runner

import (
	"context"
	"errors"
	"fmt"
	"github.com/akshay-dhangar/issuepilot/internal/runs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const buggy = `package average
func Average(values []int) int {
 total := 0
 for _, value := range values { total += value }
 return total / len(values)
}
`
const baseline = `package average
import "testing"
func TestAverage(t *testing.T) { if got:=Average([]int{2,4});got!=3 {t.Fatalf("got %d, want 3",got)} }
`
const regression = `package average
import "testing"
func TestEmptyAverage(t *testing.T) { if got:=Average(nil);got!=0 {t.Fatalf("got %d, want 0",got)} }
`
const patch = "--- a/average.go\n+++ b/average.go\n@@ -1,5 +1,6 @@\n package average\n func Average(values []int) int {\n+ if len(values) == 0 { return 0 }\n  total := 0\n  for _, value := range values { total += value }\n  return total / len(values)\n"

func Demo(ctx context.Context, r *runs.Run, save func() error) error {
	if r.Scenario != "empty-average" {
		return errors.New("unsupported fixture")
	}
	dir, err := os.MkdirTemp("", "issuepilot-fixture-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	write := func(name, body string) error { return os.WriteFile(filepath.Join(dir, name), []byte(body), 0600) }
	for name, body := range map[string]string{"go.mod": "module demo.local/average\n\ngo 1.25.0\n", "average.go": buggy, "average_test.go": baseline} {
		if err = write(name, body); err != nil {
			return err
		}
	}
	test := func() (string, error) {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cctx, "go", "test", "-count=1", "-v", "./...")
		cmd.Dir = dir
		// Only this bundled code is executed. Never forward secrets to subprocesses.
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GOCACHE=" + filepath.Join(os.TempDir(), "issuepilot-go-build"), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0"}
		cmd.WaitDelay = 2 * time.Second
		out, e := cmd.CombinedOutput()
		if cctx.Err() != nil {
			return string(out), cctx.Err()
		}
		return string(out), e
	}
	step := func(s runs.Status, msg string) error {
		if err := r.Advance(s, msg); err != nil {
			return err
		}
		return save()
	}
	r.Evidence.Baseline, err = test()
	if err != nil {
		return fmt.Errorf("baseline failed: %w", err)
	}
	if err = step(runs.Reproducing, "Existing test passed; adding an empty-input regression test."); err != nil {
		return err
	}
	if err = write("regression_test.go", regression); err != nil {
		return err
	}
	r.Evidence.Reproduction, err = test()
	if err == nil {
		return errors.New("regression did not reproduce the bug")
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || !strings.Contains(r.Evidence.Reproduction, "integer divide by zero") {
		return fmt.Errorf("unexpected reproduction failure: %w", err)
	}
	if err = step(runs.Editing, "Regression reproduced divide-by-zero; applying the bundled demo patch (not AI-generated)."); err != nil {
		return err
	}
	fixed := strings.Replace(buggy, " total := 0", " if len(values) == 0 { return 0 }\n total := 0", 1)
	if err = write("average.go", fixed); err != nil {
		return err
	}
	r.Evidence.Patch = patch
	if err = step(runs.Verifying, "Running baseline and regression tests against the patched source."); err != nil {
		return err
	}
	r.Evidence.Verification, err = test()
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}
	return step(runs.AwaitingReview, "Both tests passed. Evidence ready for review; no branch or PR was published.")
}
