package runs

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
)

func TestStateMachine(t *testing.T) {
	r := New("empty-average")
	if r.Advance(AwaitingReview, "skip") == nil {
		t.Fatal("skipped validation")
	}
	for _, s := range []Status{Investigating, Reproducing, Editing, Verifying, AwaitingReview} {
		if err := r.Advance(s, "step"); err != nil {
			t.Fatal(err)
		}
	}
	if r.Advance(Failed, "late") == nil {
		t.Fatal("mutated terminal run")
	}
}
func TestMemoryClaimExactlyOnce(t *testing.T) {
	s := NewMemory()
	ctx := context.Background()
	r := New("empty-average")
	_ = s.Create(ctx, r)
	var wg sync.WaitGroup
	var mu sync.Mutex
	n := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Claim(ctx)
			if err == nil {
				mu.Lock()
				n++
				mu.Unlock()
			} else if !errors.Is(err, ErrEmpty) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n != 1 {
		t.Fatalf("claimed %d times", n)
	}
	if err := s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, r.ID)
	if got.Status != Failed {
		t.Fatal(got.Status)
	}
}
func TestPostgresPersistenceAndRecovery(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if second, e := OpenPostgres(ctx, url); e == nil {
		second.Close()
		s.Close()
		t.Fatal("second worker acquired ownership")
	}
	r := New("empty-average")
	if err = s.Create(ctx, r); err != nil {
		s.Close()
		t.Fatal(err)
	}
	claimed, err := s.Claim(ctx)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	defer func() { _, _ = s.pool.Exec(ctx, "DELETE FROM runs WHERE id=$1", r.ID) }()
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, claimed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != Failed {
		t.Fatalf("recovered status: %s", got.Status)
	}
}

func TestPostgresQueuedRepairSurvivesRestart(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	r := NewRepair(RepairRequest{Repository: "sample", Commit: "pinned", Issue: "fix", Model: "chosen-model"})
	if err = db.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db = nil
	db, err = OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.pool.Exec(ctx, "DELETE FROM runs WHERE id=$1", r.ID) }()
	if err = db.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := db.Get(ctx, r.ID)
	if err != nil || got.Status != Queued || got.Repair == nil || *got.Repair != *r.Repair {
		t.Fatalf("queued repair changed on restart: %v", err)
	}
}
