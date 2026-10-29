package main

import (
	"context"
	"errors"
	"github.com/akshay-dhangar/issuepilot/internal/httpapi"
	"github.com/akshay-dhangar/issuepilot/internal/runner"
	"github.com/akshay-dhangar/issuepilot/internal/runs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := serve(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var store runs.Store = runs.NewMemory()
	if url := os.Getenv("DATABASE_URL"); url != "" {
		startup, cancel := context.WithTimeout(ctx, 10*time.Second)
		db, err := runs.OpenPostgres(startup, url)
		cancel()
		if err != nil {
			return err
		}
		defer db.Close()
		store = db
		slog.Info("storage mode", "mode", "postgres")
	} else {
		slog.Warn("ephemeral demo mode: runs disappear on restart; set DATABASE_URL for persistence")
	}
	recoverCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := store.Recover(recoverCtx)
	cancel()
	if err != nil {
		return err
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	server := &http.Server{Addr: addr, Handler: httpapi.Handler(store), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()
	workerDone := make(chan error, 1)
	go func() { workerDone <- runner.Work(workerCtx, store) }()
	serverDone := make(chan error, 1)
	go func() {
		slog.Info("IssuePilot local demo listening", "address", addr)
		serverDone <- server.ListenAndServe()
	}()
	var result error
	workerExited := false
	select {
	case <-ctx.Done():
	case result = <-workerDone:
		workerExited = true
	case result = <-serverDone:
		if errors.Is(result, http.ErrServerClosed) {
			result = nil
		}
	}
	workerCancel()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	if !workerExited {
		if e := <-workerDone; result == nil {
			result = e
		}
	}
	return result
}
