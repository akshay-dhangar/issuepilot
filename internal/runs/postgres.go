package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres holds a dedicated session advisory lock so a second server cannot
// recover or overwrite an active worker's jobs.
type Postgres struct {
	pool  *pgxpool.Pool
	owner *pgxpool.Conn
}

func OpenPostgres(ctx context.Context, url string) (*Postgres, error) {
	p, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	s := &Postgres{pool: p}
	ok := false
	defer func() {
		if !ok {
			s.Close()
		}
	}()
	s.owner, err = p.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var locked bool
	if err = s.owner.QueryRow(ctx, "SELECT pg_try_advisory_lock(74010421)").Scan(&locked); err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("another IssuePilot worker owns this database")
	}
	_, err = p.Exec(ctx, `CREATE TABLE IF NOT EXISTS runs (id text PRIMARY KEY, status text NOT NULL, created_at timestamptz NOT NULL, document jsonb NOT NULL); CREATE INDEX IF NOT EXISTS runs_queue ON runs(created_at) WHERE status='queued'`)
	if err != nil {
		return nil, err
	}
	ok = true
	return s, nil
}
func (s *Postgres) Close() {
	if s.owner != nil {
		c := s.owner.Hijack()
		_ = c.Close(context.Background())
	}
	if s.pool != nil {
		s.pool.Close()
	}
}
func (s *Postgres) Create(ctx context.Context, r Run) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, "INSERT INTO runs(id,status,created_at,document) VALUES($1,$2,$3,$4)", r.ID, r.Status, r.CreatedAt, b)
	return err
}
func (s *Postgres) Get(ctx context.Context, id string) (Run, error) {
	var b []byte
	err := s.pool.QueryRow(ctx, "SELECT document FROM runs WHERE id=$1", id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
	}
	var r Run
	err = json.Unmarshal(b, &r)
	return r, err
}
func (s *Postgres) Save(ctx context.Context, r Run) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, "UPDATE runs SET status=$2, document=$3 WHERE id=$1", r.ID, r.Status, b)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}
func (s *Postgres) Claim(ctx context.Context) (Run, error) {
	// Use the lock-owning connection: loss of that session stops claims.
	tx, err := s.owner.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var b []byte
	err = tx.QueryRow(ctx, "SELECT document FROM runs WHERE status='queued' ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1").Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrEmpty
	}
	if err != nil {
		return Run{}, err
	}
	var r Run
	if err = json.Unmarshal(b, &r); err != nil {
		return Run{}, err
	}
	if err = r.Advance(Investigating, "Worker claimed run."); err != nil {
		return Run{}, err
	}
	b, err = json.Marshal(r)
	if err != nil {
		return Run{}, err
	}
	if _, err = tx.Exec(ctx, "UPDATE runs SET status=$2,document=$3 WHERE id=$1", r.ID, r.Status, b); err != nil {
		return Run{}, err
	}
	return r, tx.Commit(ctx)
}
func (s *Postgres) Recover(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, "SELECT document FROM runs WHERE status NOT IN ('queued','failed','awaiting_review')")
	if err != nil {
		return err
	}
	var pending []Run
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return err
		}
		var r Run
		if err = json.Unmarshal(b, &r); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range pending {
		r.Error = "Worker stopped before completion; submit a new run."
		if err = r.Advance(Failed, r.Error); err != nil {
			return err
		}
		if err = s.Save(ctx, r); err != nil {
			return fmt.Errorf("recover %s: %w", r.ID, err)
		}
	}
	return nil
}
