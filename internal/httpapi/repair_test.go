package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akshay-dhangar/issuepilot/internal/agent"
	"github.com/akshay-dhangar/issuepilot/internal/runs"
)

const apiTestToken = "01234567890123456789012345678901"

func repairRequest(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	h.ServeHTTP(w, r)
	return w
}
func TestRepairAPIAuthenticationAndValidation(t *testing.T) {
	s := runs.NewMemory()
	h := HandlerWithOptions(s, Options{Token: apiTestToken, RepairEnabled: true, Repositories: map[string]string{"sample": "/allowed"}, Model: "pinned-model"})
	body := `{"repository":"sample","commit":"` + strings.Repeat("a", 40) + `","issue":"fix it"}`
	for _, token := range []string{"", "wrong"} {
		if w := repairRequest(h, "POST", "/v1/repairs", body, token); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	w := repairRequest(h, "POST", "/v1/repairs", body, apiTestToken)
	if w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var run runs.Run
	_ = json.Unmarshal(w.Body.Bytes(), &run)
	if run.Repair.Model != "pinned-model" {
		t.Fatal("model not pinned")
	}
	if w := repairRequest(h, "GET", "/v1/runs/"+run.ID, "", "wrong"); w.Code != 401 {
		t.Fatal("report leaked")
	}
	if w := repairRequest(Handler(s), "GET", "/v1/runs/"+run.ID, "", ""); w.Code != 503 {
		t.Fatal("repair leaked after auth disabled")
	}
	for _, invalid := range []string{strings.Replace(body, "sample", "/etc", 1), strings.Replace(body, strings.Repeat("a", 40), "HEAD", 1), strings.TrimSuffix(body, "}") + `,"model":"unapproved"}`, body + ` {}`} {
		if w := repairRequest(h, "POST", "/v1/repairs", invalid, apiTestToken); w.Code != 400 {
			t.Fatalf("accepted invalid input: %s", w.Body)
		}
	}
	if w := repairRequest(h, "GET", "/v1/runs/"+run.ID+"/patch", "", apiTestToken); w.Code != 409 {
		t.Fatal("unfinished patch exposed")
	}
	if w := repairRequest(h, "GET", "/healthz", "", ""); w.Code != 200 {
		t.Fatal("health requires auth")
	}
}
func TestPatchRequiresVerifiedDigest(t *testing.T) {
	s := runs.NewMemory()
	r := runs.NewRepair(runs.RepairRequest{Repository: "sample"})
	r.Status = runs.AwaitingReview
	patch := "test-patch\n"
	digest := sha256.Sum256([]byte(patch))
	report := agent.Report{Status: "awaiting_review", Patch: patch, PatchSHA256: hex.EncodeToString(digest[:])}
	r.Report, _ = json.Marshal(report)
	_ = s.Create(context.Background(), r)
	h := HandlerWithOptions(s, Options{Token: apiTestToken})
	w := repairRequest(h, "GET", "/v1/runs/"+r.ID+"/patch", "", apiTestToken)
	if w.Code != 200 || w.Body.String() != patch || w.Header().Get("ETag") != `"`+report.PatchSHA256+`"` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	report.Patch += "tampered"
	r.Report, _ = json.Marshal(report)
	_ = s.Save(context.Background(), r)
	if w := repairRequest(h, "GET", "/v1/runs/"+r.ID+"/patch", "", apiTestToken); w.Code != 409 {
		t.Fatal("tampered patch exposed")
	}
}
