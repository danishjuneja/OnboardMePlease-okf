package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"onboardmeplease/internal/config"
	"onboardmeplease/internal/privacy"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type EmbeddingAdapter struct {
	Kind       string
	URL        string
	Model      string
	Dimensions int
	KeyFile    string
	Client     HTTPDoer
}

func ConfiguredEmbedding(settings config.Config, kind string) (EmbeddingAdapter, error) {
	switch kind {
	case "local":
		if settings.LocalEmbeddingURL == "" || settings.LocalEmbeddingModel == "" || settings.LocalEmbeddingDimensions == 0 {
			return EmbeddingAdapter{}, errors.New("local embedding provider is not configured")
		}
		return EmbeddingAdapter{Kind: "local", URL: settings.LocalEmbeddingURL, Model: settings.LocalEmbeddingModel,
			Dimensions: settings.LocalEmbeddingDimensions}, nil
	case "cloud":
		if settings.ModelMode != "cloud_opt_in" || settings.OpenAIKeyFile == "" {
			return EmbeddingAdapter{}, errors.New("cloud embedding provider is not enabled")
		}
		return EmbeddingAdapter{Kind: "cloud", URL: "https://api.openai.com/v1/embeddings",
			Model: "text-embedding-3-small", Dimensions: 1536, KeyFile: settings.OpenAIKeyFile}, nil
	default:
		return EmbeddingAdapter{}, errors.New("unknown embedding provider")
	}
}

func (adapter EmbeddingAdapter) Generate(context.Context, privacy.ModelRequest, string) (string, error) {
	return "", errors.New("generation is not configured for the embedding adapter")
}

// Embed is the only network path for this adapter. The caller supplies the
// repository's model policy and opt-in state; the adapter fixes the destination.
func (adapter EmbeddingAdapter) Embed(ctx context.Context, request privacy.ModelRequest, input string) ([]float32, error) {
	if input == "" {
		return nil, errors.New("cannot embed empty text")
	}
	if adapter.Dimensions < 1 || adapter.Dimensions > 2000 || adapter.Model == "" {
		return nil, errors.New("embedding model configuration is invalid")
	}
	if adapter.Kind != "local" && adapter.Kind != "cloud" {
		return nil, errors.New("embedding provider is invalid")
	}
	if _, err := privacy.Clear("embedding-input", []byte(input), nil); err != nil {
		return nil, errors.New("embedding input failed privacy scan")
	}
	request.ProviderKind = adapter.Kind
	request.ProviderURL = adapter.URL
	request.ScanSucceeded = true
	request.Sanitized = true
	if err := privacy.Authorize(request); err != nil {
		return nil, err
	}
	var key string
	if adapter.Kind == "cloud" {
		if adapter.URL != "https://api.openai.com/v1/embeddings" || adapter.KeyFile == "" {
			return nil, errors.New("cloud embedding configuration is invalid")
		}
		secret, err := os.ReadFile(adapter.KeyFile)
		if err != nil {
			return nil, errors.New("cloud embedding credential unavailable")
		}
		key = strings.TrimSpace(string(secret))
		if key == "" {
			return nil, errors.New("cloud embedding credential unavailable")
		}
	}
	encoded, err := json.Marshal(map[string]any{"model": adapter.Model, "input": input, "encoding_format": "float"})
	if err != nil {
		return nil, errors.New("cannot encode embedding input")
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, adapter.URL, bytes.NewReader(encoded))
	if err != nil {
		return nil, errors.New("invalid embedding endpoint")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if key != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+key)
	}
	client := adapter.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("embedding redirect denied") }}
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return nil, errors.New("embedding provider unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("embedding provider rejected request")
	}
	var payload struct {
		Model string `json:"model"`
		Data  []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, errors.New("invalid embedding response")
	}
	if payload.Model != "" && payload.Model != adapter.Model {
		return nil, errors.New("embedding provider returned a different model")
	}
	if len(payload.Data) != 1 || len(payload.Data[0].Embedding) != adapter.Dimensions {
		return nil, errors.New("embedding dimensions do not match configuration")
	}
	for _, value := range payload.Data[0].Embedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, errors.New("embedding contains invalid numbers")
		}
	}
	return payload.Data[0].Embedding, nil
}
