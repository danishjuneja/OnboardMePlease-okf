package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"onboardmeplease/internal/privacy"
	"strings"
	"testing"
)

var testSchema = json.RawMessage(`{"type":"object","properties":{"claims":{"type":"array","items":{"type":"string"}}},"required":["claims"],"additionalProperties":false}`)

func TestGenerationBlocksEgressBeforeCredentialRead(t *testing.T) {
	adapter := GenerationAdapter{Kind: "cloud", URL: "https://api.openai.com/v1/responses", Model: "test", KeyFile: "missing"}
	for _, policy := range []privacy.ModelRequest{{Mode: "strict_local", RepoOptedIn: true}, {Mode: "cloud_opt_in"}} {
		_, err := adapter.JSON(context.Background(), policy, "instructions", map[string]string{"text": "safe"}, testSchema)
		if err != privacy.ErrEgressDenied {
			t.Fatalf("expected privacy denial before network or credential access: %v", err)
		}
	}
}

func TestGenerationLocalStructuredContractAndFailures(t *testing.T) {
	response := `{"choices":[{"finish_reason":"stop","message":{"content":"{\"claims\":[]}"}}]}`
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request map[string]any
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid request")
		}
		if request["response_format"] == nil || request["messages"] == nil || r.Header.Get("Authorization") != "" {
			t.Error("invalid local generation contract")
		}
		io.WriteString(w, response)
	}))
	defer server.Close()
	adapter := GenerationAdapter{Kind: "local", URL: server.URL, Model: "test"}
	raw, err := adapter.JSON(context.Background(), privacy.ModelRequest{Mode: "strict_local"}, "rules", map[string]string{"source": "safe"}, testSchema)
	if err != nil || string(raw) != `{"claims":[]}` {
		t.Fatalf("generation failed: %v", err)
	}
	for _, bad := range []string{`{"choices":[{"finish_reason":"length","message":{"content":"{}"}}]}`, `{"choices":[{"finish_reason":"stop","message":{"content":"not JSON"}}]}`, `{"choices":[{"finish_reason":"stop","message":{"content":"{}","refusal":"no"}}]}`} {
		response = bad
		if _, err = adapter.JSON(context.Background(), privacy.ModelRequest{Mode: "strict_local"}, "rules", "safe", testSchema); err == nil {
			t.Fatal("invalid output accepted")
		}
	}
	before := calls
	if _, err = adapter.JSON(context.Background(), privacy.ModelRequest{Mode: "strict_local"}, "rules", "api_key=synthetic-secret-value", testSchema); err == nil || calls != before {
		t.Fatal("secret input reached provider")
	}
}

func TestGenerationRedirectAndErrorBodyNotExposed(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	adapter := GenerationAdapter{Kind: "local", URL: source.URL, Model: "test"}
	_, err := adapter.JSON(context.Background(), privacy.ModelRequest{Mode: "strict_local"}, "rules", "safe", testSchema)
	if err == nil || targetCalls != 0 {
		t.Fatal("redirect followed")
	}
	failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		io.WriteString(w, "sensitive provider body")
	}))
	defer failure.Close()
	adapter.URL = failure.URL
	_, err = adapter.JSON(context.Background(), privacy.ModelRequest{Mode: "strict_local"}, "rules", "safe", testSchema)
	if err == nil || strings.Contains(err.Error(), "sensitive") || !strings.Contains(err.Error(), "429") {
		t.Fatal("unsafe error handling")
	}
}

func TestGenerationDistinguishesQuotaFromRateLimit(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{"insufficient_quota", "generation API quota exhausted (HTTP 429)"},
		{"rate_limit_exceeded", "generation rate limit exceeded (HTTP 429); wait before resuming"},
		{"unknown", "generation quota or rate limit exceeded (HTTP 429)"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(429)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": tc.code, "message": "private response detail"}})
			}))
			defer server.Close()
			a := GenerationAdapter{Kind: "local", URL: server.URL, Model: "test"}
			_, err := a.JSON(context.Background(), privacy.ModelRequest{Mode: "strict_local"}, "rules", "safe", testSchema)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
