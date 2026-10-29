package httpapi

import (
	"encoding/json"
	"github.com/akshay-dhangar/issuepilot/internal/runs"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateAndRead(t *testing.T) {
	h := Handler(runs.NewMemory())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/runs", strings.NewReader(`{"scenario":"empty-average"}`)))
	if w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var run runs.Run
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	read := httptest.NewRecorder()
	h.ServeHTTP(read, httptest.NewRequest("GET", w.Header().Get("Location"), nil))
	if read.Code != 200 || !strings.Contains(read.Body.String(), run.ID) {
		t.Fatalf("read: %s", read.Body)
	}
}
func TestRejectInvalidRequests(t *testing.T) {
	h := Handler(runs.NewMemory())
	for _, body := range []string{`{}`, `{"scenario":"unknown"}`, `{"scenario":"empty-average","repository":"/etc"}`, `{"scenario":"empty-average"} {}`, `{"scenario":`, `null`} {
		t.Run(body, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/runs", strings.NewReader(body)))
			if w.Code != 400 {
				t.Fatalf("got %d", w.Code)
			}
		})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/runs", strings.NewReader(`{"scenario":"`+strings.Repeat("x", 5000)+`"}`)))
	if w.Code != 413 {
		t.Fatalf("oversize: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/v1/runs/missing", nil))
	if w.Code != 404 {
		t.Fatalf("missing: %d", w.Code)
	}
}
