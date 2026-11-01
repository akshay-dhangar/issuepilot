package runner

import (
	"context"
	"errors"
	"fmt"
	"github.com/akshay-dhangar/issuepilot/internal/runs"
	"log/slog"
	"time"
)

type Executor func(context.Context, *runs.Run, func() error) error

func Work(ctx context.Context, s runs.Store, repair ...Executor) error {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			r, err := s.Claim(ctx)
			if errors.Is(err, runs.ErrEmpty) {
				continue
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("claim: %w", err)
			}
			budget := 2 * time.Minute
			execute := Executor(Demo)
			if r.Repair != nil {
				budget = 11 * time.Minute
				if len(repair) > 0 && repair[0] != nil {
					execute = repair[0]
				} else {
					execute = func(context.Context, *runs.Run, func() error) error {
						return errors.New("repair execution is disabled")
					}
				}
			}
			runctx, cancel := context.WithTimeout(ctx, budget)
			err = execute(runctx, &r, func() error { return s.Save(runctx, r) })
			cancel()
			if err != nil {
				r.Error = err.Error()
				if r.Status != runs.Failed {
					// Executor errors always invalidate terminal success, including observer failures.
					if r.Status == runs.AwaitingReview {
						r.Status = runs.Repairing
					}
					r.Evidence.Patch = ""
					_ = r.Advance(runs.Failed, "Execution stopped: "+err.Error())
				}
			}
			persist, persistCancel := context.WithTimeout(context.Background(), 5*time.Second)
			saveErr := s.Save(persist, r)
			persistCancel()
			if saveErr != nil {
				return fmt.Errorf("save completed run: %w", saveErr)
			}
			slog.Info("run finished", "id", r.ID, "status", r.Status)
		}
	}
}
