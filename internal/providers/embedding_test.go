package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"onboardmeplease/internal/privacy"
)

func TestLocalEmbeddingAdapterUsesPolicyAndChecksDimensions(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" {
			t.Error("unexpected request")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"input":"safe text"`) {
			t.Errorf("unexpected body: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer server.Close()
	adapter := EmbeddingAdapter{Kind: "local", URL: server.URL, Model: "test-model", Dimensions: 3}
	vector, err := adapter.Embed(context.Background(), privacy.ModelRequest{Mode: "strict_local"}, "safe text")
	if err != nil || len(vector) != 3 || calls != 1 {
		t.Fatalf("embedding: %v, %v, calls=%d", vector, err, calls)
	}
	if _, err := adapter.Embed(context.Background(), privacy.ModelRequest{Mode: "strict_local"}, "api_key=synthetic-secret-value"); err == nil {
		t.Fatal("secret-bearing input was sent")
	}
	if calls != 1 {
		t.Fatalf("privacy failure contacted provider: %d", calls)
	}
	adapter.Dimensions = 4
	if _, err := adapter.Embed(context.Background(), privacy.ModelRequest{Mode: "strict_local"}, "safe text"); err == nil {
		t.Fatal("mismatched dimensions accepted")
	}
}

func TestStrictLocalCloudAdapterCannotEgress(t *testing.T) {
	adapter := EmbeddingAdapter{Kind: "cloud", URL: "https://api.openai.com/v1/embeddings", Model: "text-embedding-3-small", Dimensions: 1536, KeyFile: "missing"}
	if _, err := adapter.Embed(context.Background(), privacy.ModelRequest{Mode: "strict_local", RepoOptedIn: true}, "safe text"); err == nil {
		t.Fatal("strict local mode allowed cloud embedding")
	}
}
