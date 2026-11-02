package runner

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/akshay-dhangar/issuepilot/internal/agent"
	"github.com/akshay-dhangar/issuepilot/internal/runs"
)

type RepairExecutor struct {
	Engine       agent.Engine
	Repositories map[string]string
	Model        string
}

func (e RepairExecutor) Execute(ctx context.Context, r *runs.Run, save func() error) error {
	if r.Repair == nil {
		return errors.New("missing repair request")
	}
	repository, ok := e.Repositories[r.Repair.Repository]
	if !ok || repository == "" {
		return errors.New("repository is no longer enabled")
	}
	if r.Repair.Model != e.Model {
		return errors.New("queued model differs from configured model; resubmit the run")
	}
	if err := r.Advance(runs.Repairing, "Starting bounded model workflow in Docker."); err != nil {
		return err
	}
	if err := save(); err != nil {
		return err
	}
	engine := e.Engine
	engine.Observe = func(message string) error { r.Record(message); return save() }
	report, runErr := engine.Run(ctx, repository, r.Repair.Commit, r.Repair.Issue)
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	r.Report = data
	if runErr != nil {
		return runErr
	}
	if report.Status != "awaiting_review" || report.Patch == "" {
		return errors.New("workflow returned no verified patch")
	}
	r.Evidence.Patch = report.Patch
	return r.Advance(runs.AwaitingReview, "Verified patch and execution report ready for review.")
}
