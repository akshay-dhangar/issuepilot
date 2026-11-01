package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akshay-dhangar/issuepilot/internal/runs"
)

func TestWorkerDispatchesAndPersistsRepair(t *testing.T) {
	s := runs.NewMemory()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := runs.NewRepair(runs.RepairRequest{Repository: "sample", Commit: "commit", Model: "model"})
	_ = s.Create(ctx, r)
	execute := func(ctx context.Context, r *runs.Run, save func() error) error {
		if r.Repair.Repository != "sample" {
			t.Error("wrong job")
		}
		if err := r.Advance(runs.Repairing, "starting"); err != nil {
			return err
		}
		r.Report = []byte(`{"status":"failed","error":"scripted failure"}`)
		return errors.New("scripted failure")
	}
	done := make(chan error, 1)
	go func() { done <- Work(ctx, s, execute) }()
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("worker did not finish")
		case <-ticker.C:
			got, _ := s.Get(ctx, r.ID)
			if got.Status == runs.Failed {
				if len(got.Report) == 0 || got.Error != "scripted failure" {
					t.Fatalf("evidence lost: %+v", got)
				}
				cancel()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				return
			}
		}
	}
}
func TestMemoryDeepCopiesRepairEvidence(t *testing.T) {
	s := runs.NewMemory()
	ctx := context.Background()
	r := runs.NewRepair(runs.RepairRequest{Repository: "original"})
	r.Report = []byte(`{}`)
	_ = s.Create(ctx, r)
	copy, _ := s.Get(ctx, r.ID)
	copy.Repair.Repository = "modified"
	copy.Report[0] = 'x'
	got, _ := s.Get(ctx, r.ID)
	if got.Repair.Repository != "original" || string(got.Report) != "{}" {
		t.Fatal("shared mutable state")
	}
}

func TestRepairExecutorRejectsChangedConfiguration(t *testing.T) {
	r := runs.NewRepair(runs.RepairRequest{Repository: "sample", Model: "old-model"})
	executor := RepairExecutor{Repositories: map[string]string{"sample": "/approved"}, Model: "new-model"}
	saved := false
	if err := executor.Execute(context.Background(), &r, func() error { saved = true; return nil }); err == nil || saved {
		t.Fatal("queued model silently changed")
	}
	executor.Model = "old-model"
	executor.Repositories = map[string]string{}
	if err := executor.Execute(context.Background(), &r, func() error { saved = true; return nil }); err == nil || saved {
		t.Fatal("removed repository executed")
	}
}
