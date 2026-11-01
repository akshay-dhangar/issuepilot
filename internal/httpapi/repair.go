package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/akshay-dhangar/issuepilot/internal/agent"
	"github.com/akshay-dhangar/issuepilot/internal/runs"
)

type Options struct {
	Token         string
	RepairEnabled bool
	Repositories  map[string]string
	Model         string
}

var commitSHA = regexp.MustCompile(`^[a-f0-9]{40}$`)

func registerRepairs(mux *http.ServeMux, store runs.Store, options Options) {
	mux.HandleFunc("POST /v1/repairs", func(w http.ResponseWriter, r *http.Request) {
		if !options.RepairEnabled || len(options.Token) < 32 {
			reply(w, 503, map[string]string{"error": "repair service is disabled"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		var input struct {
			Repository string `json:"repository"`
			Commit     string `json:"commit"`
			Issue      string `json:"issue"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&input)
		if err == nil {
			var extra any
			err = decoder.Decode(&extra)
			if err == io.EOF {
				err = nil
			} else if err == nil {
				err = errors.New("trailing JSON")
			}
		}
		if err != nil {
			var max *http.MaxBytesError
			if errors.As(err, &max) {
				reply(w, 413, map[string]string{"error": "body exceeds 16384 bytes"})
			} else {
				reply(w, 400, map[string]string{"error": "expected repository, commit and issue only"})
			}
			return
		}
		if _, ok := options.Repositories[input.Repository]; !ok {
			reply(w, 400, map[string]string{"error": "repository alias is not enabled"})
			return
		}
		if !commitSHA.MatchString(input.Commit) || strings.TrimSpace(input.Issue) == "" || len(input.Issue) > 8192 || !utf8.ValidString(input.Issue) {
			reply(w, 400, map[string]string{"error": "a full lowercase commit SHA and 1-8192 byte UTF-8 issue are required"})
			return
		}
		run := runs.NewRepair(runs.RepairRequest{Repository: input.Repository, Commit: input.Commit, Issue: input.Issue, Model: options.Model})
		if err = store.Create(r.Context(), run); err != nil {
			reply(w, 503, map[string]string{"error": "storage unavailable"})
			return
		}
		w.Header().Set("Location", "/v1/runs/"+run.ID)
		reply(w, 202, run)
	})
	mux.HandleFunc("GET /v1/runs/{id}/patch", func(w http.ResponseWriter, r *http.Request) {
		if options.Token == "" {
			reply(w, 503, map[string]string{"error": "authenticated access must be configured"})
			return
		}
		run, err := store.Get(r.Context(), r.PathValue("id"))
		if errors.Is(err, runs.ErrNotFound) {
			reply(w, 404, map[string]string{"error": "run not found"})
			return
		}
		if err != nil {
			reply(w, 503, map[string]string{"error": "storage unavailable"})
			return
		}
		if run.Repair == nil || run.Status != runs.AwaitingReview {
			reply(w, 409, map[string]string{"error": "no verified repair patch is available"})
			return
		}
		var report agent.Report
		if json.Unmarshal(run.Report, &report) != nil || report.Status != "awaiting_review" || report.Patch == "" {
			reply(w, 409, map[string]string{"error": "verified patch evidence is unavailable"})
			return
		}
		sum := sha256.Sum256([]byte(report.Patch))
		digest := hex.EncodeToString(sum[:])
		if digest != report.PatchSHA256 {
			reply(w, 409, map[string]string{"error": "patch digest does not match verification report"})
			return
		}
		// Return a digest for clients to verify against the stored report.
		w.Header().Set("Content-Type", "text/x-diff; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="patch.diff"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("ETag", `"`+digest+`"`)
		_, _ = io.WriteString(w, report.Patch)
	})
}
func authenticate(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	expected := sha256.Sum256([]byte("Bearer " + token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			reply(w, 401, map[string]string{"error": "authentication required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
