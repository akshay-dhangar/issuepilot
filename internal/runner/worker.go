package runner

import (
	"context"
	"errors"
	"fmt"
	"github.com/akshay-dhangar/issuepilot/internal/runs"
	"log/slog"
	"time"
)

func Work(ctx context.Context, s runs.Store) error {
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
			runctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			err = Demo(runctx, &r, func() error { return s.Save(runctx, r) })
			cancel()
			if err != nil {
				r.Error = err.Error()
				if r.Status != runs.AwaitingReview && r.Status != runs.Failed {
					_ = r.Advance(runs.Failed, "Execution stopped: "+err.Error())
				}
				persist, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				saveErr := s.Save(persist, r)
				cancel()
				if saveErr != nil {
					return fmt.Errorf("save failed run: %w", saveErr)
				}
			}
			slog.Info("run finished", "id", r.ID, "status", r.Status)
		}
	}
}
