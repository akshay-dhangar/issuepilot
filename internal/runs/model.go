package runs

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Status string

const (
	Queued         Status = "queued"
	Investigating  Status = "investigating"
	Reproducing    Status = "reproducing"
	Editing        Status = "editing"
	Verifying      Status = "verifying"
	AwaitingReview Status = "awaiting_review"
	Failed         Status = "failed"
	Repairing      Status = "repairing"
)

var ErrNotFound = errors.New("run not found")
var ErrEmpty = errors.New("no queued runs")

type Event struct {
	At      time.Time `json:"at"`
	Status  Status    `json:"status"`
	Message string    `json:"message"`
}
type Evidence struct {
	Baseline     string `json:"baseline,omitempty"`
	Reproduction string `json:"reproduction,omitempty"`
	Verification string `json:"verification,omitempty"`
	Patch        string `json:"patch,omitempty"`
}
type RepairRequest struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Issue      string `json:"issue"`
	Model      string `json:"model"`
}

type Run struct {
	Repair    *RepairRequest  `json:"repair,omitempty"`
	Report    json.RawMessage `json:"report,omitempty"`
	ID        string          `json:"id"`
	Scenario  string          `json:"scenario"`
	Status    Status          `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Events    []Event         `json:"events"`
	Evidence  Evidence        `json:"evidence"`
	Error     string          `json:"error,omitempty"`
}

func New(scenario string) Run {
	now := time.Now().UTC()
	return Run{ID: fmt.Sprintf("%x", rand.Text()), Scenario: scenario, Status: Queued, CreatedAt: now, UpdatedAt: now, Events: []Event{{now, Queued, "Bundled deterministic demo queued; no model is involved."}}}
}
func (r *Run) Advance(next Status, message string) error {
	valid := map[Status]Status{Queued: Investigating, Investigating: Reproducing, Reproducing: Editing, Editing: Verifying, Verifying: AwaitingReview}
	if next != Failed && valid[r.Status] != next && !(r.Status == Investigating && next == Repairing) && !(r.Status == Repairing && next == AwaitingReview) {
		return fmt.Errorf("invalid transition %s -> %s", r.Status, next)
	}
	if r.Status == Failed || r.Status == AwaitingReview {
		return errors.New("run is terminal")
	}
	r.Status = next
	r.UpdatedAt = time.Now().UTC()
	r.Events = append(r.Events, Event{r.UpdatedAt, next, message})
	return nil
}

// Store has one active worker per database. Claim is atomic; Recover is called
// once at startup while holding exclusive ownership of the worker.
type Store interface {
	Create(context.Context, Run) error
	Get(context.Context, string) (Run, error)
	Claim(context.Context) (Run, error)
	Save(context.Context, Run) error
	Recover(context.Context) error
}

func NewRepair(request RepairRequest) Run {
	r := New("")
	r.Repair = &request
	r.Events[0].Message = "Model repair queued for configured repository and pinned commit."
	return r
}
func (r *Run) Record(message string) {
	r.UpdatedAt = time.Now().UTC()
	r.Events = append(r.Events, Event{r.UpdatedAt, r.Status, message})
}
