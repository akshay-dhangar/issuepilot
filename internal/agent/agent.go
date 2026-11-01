// Package agent coordinates bounded model proposals and independently executed tests.
package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akshay-dhangar/issuepilot/internal/model"
	"github.com/akshay-dhangar/issuepilot/internal/sandbox"
)

type Tester interface {
	TestJSON(context.Context, sandbox.Snapshot) (sandbox.Result, error)
}
type Attempt struct {
	Number       int             `json:"number"`
	Proposal     model.Proposal  `json:"proposal"`
	Verification *sandbox.Result `json:"verification,omitempty"`
	Error        string          `json:"error,omitempty"`
}
type Report struct {
	Status             string          `json:"status"`
	Commit             string          `json:"commit"`
	Issue              string          `json:"issue"`
	StartedAt          time.Time       `json:"started_at"`
	FinishedAt         time.Time       `json:"finished_at"`
	Events             []string        `json:"events"`
	Baseline           *sandbox.Result `json:"baseline,omitempty"`
	RegressionProposal *model.Proposal `json:"regression_proposal,omitempty"`
	Reproduction       *sandbox.Result `json:"reproduction,omitempty"`
	Attempts           []Attempt       `json:"attempts"`
	Patch              string          `json:"patch,omitempty"`
	PatchSHA256        string          `json:"patch_sha256,omitempty"`
	RegressionSHA256   string          `json:"regression_sha256,omitempty"`
	Error              string          `json:"error,omitempty"`
}
type Engine struct {
	Provider    model.Provider
	Tester      Tester
	MaxAttempts int
	Observe     func(string) error
}

func (e Engine) Run(ctx context.Context, repository, commit, issue string) (report Report, err error) {
	report = Report{Status: "failed", Commit: commit, Issue: issue, StartedAt: time.Now().UTC(), Events: []string{}, Attempts: []Attempt{}}
	defer func() {
		report.FinishedAt = time.Now().UTC()
		if err != nil {
			report.Error = err.Error()
			report.Status = "failed"
			report.Patch = ""
			report.PatchSHA256 = ""
		}
	}()
	if e.Provider == nil || e.Tester == nil {
		return report, errors.New("provider and tester are required")
	}
	if e.MaxAttempts < 1 || e.MaxAttempts > 2 {
		return report, errors.New("repair attempts must be 1 or 2")
	}
	if strings.TrimSpace(issue) == "" || len(issue) > 8192 || !utf8.ValidString(issue) {
		return report, errors.New("issue must contain 1-8192 bytes of UTF-8 text")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	note := func(message string) error {
		report.Events = append(report.Events, message)
		if e.Observe != nil {
			return e.Observe(message)
		}
		return nil
	}
	original, err := sandbox.Prepare(ctx, repository, commit)
	if err != nil {
		return report, err
	}
	defer original.Close()
	files, err := contextFiles(original.Directory)
	if err != nil {
		return report, err
	}
	packageName, err := rootModule(files)
	if err != nil {
		return report, err
	}
	if err = note("Running baseline on the pinned commit."); err != nil {
		return report, err
	}
	baseline, err := e.Tester.TestJSON(ctx, original)
	report.Baseline = &baseline
	if err != nil {
		return report, fmt.Errorf("baseline infrastructure: %w", err)
	}
	if baseline.ExitCode != 0 || baseline.Truncated {
		return report, errors.New("baseline must pass with complete output before proposing a regression")
	}
	if err = note("Investigating issue and requesting one regression test from the model."); err != nil {
		return report, err
	}
	proposal, err := e.Provider.Propose(ctx, model.Request{Stage: "regression", Issue: issue, Files: files})
	if err != nil {
		return report, fmt.Errorf("regression proposal: %w", err)
	}
	report.RegressionProposal = &proposal
	regression, err := validateRegression(proposal)
	if err != nil {
		return report, err
	}
	report.RegressionSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(regression.Content)))
	repro, err := sandbox.Prepare(ctx, repository, commit)
	if err != nil {
		return report, err
	}
	defer repro.Close()
	if err = writeFiles(repro.Directory, []model.File{regression}); err != nil {
		return report, err
	}
	reproduction, err := e.Tester.TestJSON(ctx, repro)
	report.Reproduction = &reproduction
	if err != nil {
		return report, fmt.Errorf("reproduction infrastructure: %w", err)
	}
	if reproduction.ExitCode != 1 || reproduction.Truncated || !testEvent(reproduction.Output, "fail", packageName) {
		return report, errors.New("regression must execute and fail; build errors, skipped tests, and truncated evidence do not qualify")
	}
	if err = note("Named regression failed on original code; freezing that test for every repair attempt."); err != nil {
		return report, err
	}
	feedback := bounded(reproduction.Output, 16384)
	for attempt := 1; attempt <= e.MaxAttempts; attempt++ {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		inputs := append(append([]model.File(nil), files...), regression)
		proposed, proposalErr := e.Provider.Propose(ctx, model.Request{Stage: "repair", Issue: issue, Files: inputs, Feedback: feedback})
		entry := Attempt{Number: attempt, Proposal: proposed}
		if err = note(fmt.Sprintf("Received repair proposal %d of %d; validating candidate.", attempt, e.MaxAttempts)); err != nil {
			return report, err
		}
		if proposalErr != nil {
			entry.Error = proposalErr.Error()
			report.Attempts = append(report.Attempts, entry)
			return report, fmt.Errorf("repair proposal: %w", proposalErr)
		}
		edits, validationErr := validateFix(proposed, files)
		if validationErr != nil {
			entry.Error = validationErr.Error()
			report.Attempts = append(report.Attempts, entry)
			feedback = "Previous proposal was rejected: " + entry.Error
			continue
		}
		candidate, prepErr := sandbox.Prepare(ctx, repository, commit)
		if prepErr != nil {
			return report, prepErr
		}
		changes := append(append([]model.File(nil), edits...), regression)
		if writeErr := writeFiles(candidate.Directory, changes); writeErr != nil {
			_ = candidate.Close()
			return report, writeErr
		}
		verified, verifyErr := e.Tester.TestJSON(ctx, candidate)
		_ = candidate.Close()
		entry.Verification = &verified
		if verifyErr != nil {
			entry.Error = verifyErr.Error()
			report.Attempts = append(report.Attempts, entry)
			return report, fmt.Errorf("verification infrastructure: %w", verifyErr)
		}
		if verified.ExitCode == 0 && !verified.Truncated && testEvent(verified.Output, "pass", packageName) {
			report.Attempts = append(report.Attempts, entry)
			report.Patch = fullPatch(files, changes)
			report.PatchSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(report.Patch)))
			report.Status = "awaiting_review"
			if err = note("Existing suite and frozen regression passed; patch is ready for human review, not automatically published."); err != nil {
				return report, err
			}
			return report, nil
		}
		entry.Error = "verification did not pass the full suite and named regression with complete evidence"
		report.Attempts = append(report.Attempts, entry)
		// Every attempt starts from the original commit, so include the previous
		// proposal with its failure evidence rather than implying it is current source.
		prior, _ := json.Marshal(proposed.Files)
		feedback = "Previous candidate (not current source):\n" + bounded(string(prior), 32768) + "\nFailure:\n" + bounded(verified.Output, 16384)
	}
	return report, errors.New("repair attempt budget exhausted")
}
func bounded(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n[truncated for model context]"
	}
	return s
}
func testEvent(output, action, packageName string) bool {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	found := false
	for scanner.Scan() {
		var event struct {
			Action  string
			Test    string
			Package string
		}
		if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Test == regressionTest && event.Action == action && event.Package == packageName {
			found = true
		}
	}
	return scanner.Err() == nil && found
}

func rootModule(files []model.File) (string, error) {
	re := regexp.MustCompile(`(?m)^module[ \t]+("[^"\r\n]+"|[^ \t\r\n]+)`)
	for _, f := range files {
		if f.Path == "go.mod" {
			match := re.FindStringSubmatch(f.Content)
			if len(match) != 2 {
				break
			}
			name := match[1]
			if strings.HasPrefix(name, "\"") {
				decoded, err := strconv.Unquote(name)
				if err != nil {
					return "", err
				}
				name = decoded
			}
			return name, nil
		}
	}
	return "", errors.New("root go.mod must declare a module")
}
