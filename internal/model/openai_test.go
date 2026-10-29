package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testClient(t *testing.T, handler http.HandlerFunc) *OpenAI {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	p, err := NewOpenAI("test-key", "chosen-model")
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(server.URL)
	p.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != endpoint {
			t.Errorf("unexpected provider URL %s", r.URL)
		}
		copy := r.Clone(r.Context())
		copy.URL = target
		return http.DefaultTransport.RoundTrip(copy)
	})
	return p
}
func envelope(content string) string {
	b, _ := json.Marshal(map[string]any{"id": "resp_test", "model": "chosen-model", "status": "completed", "usage": map[string]int{"input_tokens": 12, "output_tokens": 34}, "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": content}}}}})
	return string(b)
}
func TestResponsesContract(t *testing.T) {
	p := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("request auth or method incorrect")
		}
		var body struct {
			Model           string
			Store           bool
			MaxOutputTokens int `json:"max_output_tokens"`
			Instructions    string
			Input           string
			Text            struct {
				Format struct {
					Type   string
					Strict bool
					Schema map[string]any
				}
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "chosen-model" || body.Store || body.MaxOutputTokens != 8192 || body.Text.Format.Type != "json_schema" || !body.Text.Format.Strict || body.Text.Format.Schema["additionalProperties"] != false {
			t.Errorf("invalid request: %+v", body)
		}
		if !strings.Contains(body.Instructions, "untrusted data") {
			t.Error("missing trust boundary")
		}
		_, _ = io.WriteString(w, envelope(`{"summary":"diagnosis","files":[{"path":"x.go","content":"package x\n"}]}`))
	})
	got, err := p.Propose(context.Background(), Request{Stage: "repair", Issue: "fix"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ResponseID != "resp_test" || got.Usage.InputTokens != 12 || len(got.Files) != 1 {
		t.Fatalf("bad proposal: %+v", got)
	}
}
func TestProviderRejectsUnusableResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"refusal", 200, `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal"}]}]}`},
		{"incomplete", 200, `{"status":"incomplete","output":[]}`},
		{"invalid-json", 200, envelope(`not JSON`)},
		{"unknown-field", 200, envelope(`{"summary":"x","files":[],"command":"rm"}`)},
		{"trailing", 200, envelope(`{"summary":"x","files":[]} {}`)},
		{"error-body", 429, "secret-sentinel"},
		{"oversized", 200, strings.Repeat("x", (1<<20)+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := p.Propose(context.Background(), Request{Stage: "regression"})
			if err == nil {
				t.Fatal("invalid response accepted")
			}
			if strings.Contains(err.Error(), "secret-sentinel") {
				t.Fatal("upstream error content leaked")
			}
		})
	}
}
func TestProviderRequiresConfiguration(t *testing.T) {
	if _, err := NewOpenAI("", "model"); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, err := NewOpenAI("key", ""); err == nil {
		t.Fatal("missing model accepted")
	}
}
func TestCancelledRequest(t *testing.T) {
	p := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("cancelled request sent") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Propose(ctx, Request{Stage: "repair"}); err == nil {
		t.Fatal("cancellation ignored")
	}
}
