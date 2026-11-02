package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/akshay-dhangar/issuepilot/internal/runs"
	"io"
	"log/slog"
	"net/http"
)

func Handler(store runs.Store) http.Handler { return HandlerWithOptions(store, Options{}) }

func HandlerWithOptions(store runs.Store, options Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /v1/runs", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var input struct {
			Scenario string `json:"scenario"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		err := dec.Decode(&input)
		if err == nil {
			var extra any
			if e := dec.Decode(&extra); e != io.EOF {
				if e == nil {
					err = errors.New("multiple JSON values")
				} else {
					err = e
				}
			}
		}
		if err != nil {
			var large *http.MaxBytesError
			if errors.As(err, &large) {
				reply(w, 413, map[string]string{"error": "body exceeds 4096 bytes"})
				return
			}
			reply(w, 400, map[string]string{"error": "expected one JSON object containing only scenario"})
			return
		}
		if input.Scenario != "empty-average" {
			reply(w, 400, map[string]string{"error": "scenario must be empty-average"})
			return
		}
		run := runs.New(input.Scenario)
		if err = store.Create(r.Context(), run); err != nil {
			slog.Error("create run", "error", err)
			reply(w, 503, map[string]string{"error": "storage unavailable"})
			return
		}
		w.Header().Set("Location", "/v1/runs/"+run.ID)
		reply(w, 202, run)
	})
	mux.HandleFunc("GET /v1/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		run, err := store.Get(r.Context(), r.PathValue("id"))
		if errors.Is(err, runs.ErrNotFound) {
			reply(w, 404, map[string]string{"error": "run not found"})
			return
		}
		if err != nil {
			slog.Error("get run", "error", err)
			reply(w, 503, map[string]string{"error": "storage unavailable"})
			return
		}
		if run.Repair != nil && options.Token == "" {
			reply(w, 503, map[string]string{"error": "authenticated access must be configured"})
			return
		}
		reply(w, 200, run)
	})
	registerRepairs(mux, store, options)
	return authenticate(mux, options.Token)
}
func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
