package runner

import (
	"context"
	"github.com/akshay-dhangar/issuepilot/internal/runs"
	"strings"
	"testing"
)

func TestDemoProducesRealEvidence(t *testing.T) {
	r := runs.New("empty-average")
	_ = r.Advance(runs.Investigating, "claimed")
	if err := Demo(context.Background(), &r, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if r.Status != runs.AwaitingReview {
		t.Fatal(r.Status)
	}
	if !strings.Contains(r.Evidence.Reproduction, "integer divide by zero") || !strings.Contains(r.Evidence.Verification, "PASS") || r.Evidence.Patch == "" {
		t.Fatalf("missing evidence: %+v", r.Evidence)
	}
}
func TestCancelledDemoDoesNotSucceed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := runs.New("empty-average")
	_ = r.Advance(runs.Investigating, "claimed")
	if err := Demo(ctx, &r, func() error { return nil }); err == nil {
		t.Fatal("cancelled run succeeded")
	}
	if r.Status == runs.AwaitingReview {
		t.Fatal("cancelled run awaiting review")
	}
}
