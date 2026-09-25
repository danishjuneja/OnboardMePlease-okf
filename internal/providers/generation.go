package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"onboardmeplease/internal/config"
	"onboardmeplease/internal/privacy"
)

type Generator interface {
	JSON(context.Context, privacy.ModelRequest, string, any, json.RawMessage) ([]byte, error)
}

type GenerationAdapter struct {
	Kind, URL, Model, KeyFile string
	Client                    HTTPDoer
}

func ConfiguredGeneration(c config.Config) (GenerationAdapter, error) {
	a := GenerationAdapter{Kind: c.GenerationProvider, Model: c.GenerationModel, KeyFile: c.OpenAIKeyFile}
	if a.Model == "" {
		return a, errors.New("GENERATION_MODEL is not configured")
	}
	switch a.Kind {
	case "cloud":
		if c.ModelMode != "cloud_opt_in" || a.KeyFile == "" {
			return a, errors.New("cloud generation is not enabled")
		}
		a.URL = "https://api.openai.com/v1/responses"
	case "local":
		a.URL = c.LocalGenerationURL
		if a.URL == "" {
			return a, errors.New("LOCAL_GENERATION_URL is not configured")
		}
	default:
		return a, errors.New("GENERATION_PROVIDER must be local or cloud")
	}
	return a, nil
}

// JSON has no tools, redirect following, provider fallback, or provider-body logging.
func (a GenerationAdapter) JSON(ctx context.Context, policy privacy.ModelRequest, instructions string, input any, schema json.RawMessage) ([]byte, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, errors.New("invalid generation input")
	}
	if len(raw) > 160000 || len(instructions) > 64000 || !json.Valid(schema) {
		return nil, errors.New("generation request exceeds contract")
	}
	if _, err = privacy.Clear("generation-input", raw, nil); err != nil {
		return nil, errors.New("generation input failed privacy scan")
	}
	policy.ProviderKind = a.Kind
	policy.ProviderURL = a.URL
	policy.ScanSucceeded = true
	policy.Sanitized = true
	if err = privacy.Authorize(policy); err != nil {
		return nil, err
	}
	var payload any
	format := map[string]any{"type": "json_schema", "name": "source_grounded_output", "strict": true, "schema": schema}
	if a.Kind == "cloud" {
		if a.URL != "https://api.openai.com/v1/responses" {
			return nil, errors.New("invalid cloud generation destination")
		}
		payload = map[string]any{"model": a.Model, "store": false, "instructions": instructions, "input": string(raw), "text": map[string]any{"format": format}, "max_output_tokens": 6000}
	} else {
		payload = map[string]any{"model": a.Model, "messages": []map[string]string{{"role": "system", "content": instructions}, {"role": "user", "content": string(raw)}}, "response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "source_grounded_output", "strict": true, "schema": schema}}, "max_tokens": 6000}
	}
	if a.Kind == "cloud" && strings.HasPrefix(a.Model, "gpt-5") {
		payload.(map[string]any)["reasoning"] = map[string]any{"effort": "low"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("cannot encode generation request")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid generation endpoint")
	}
	request.Header.Set("Content-Type", "application/json")
	if a.Kind == "cloud" {
		key, e := os.ReadFile(a.KeyFile)
		if e != nil || len(bytes.TrimSpace(key)) == 0 {
			return nil, errors.New("generation credential unavailable")
		}
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(key)))
	}
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("generation redirect denied") }}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("generation provider unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == 429 {
			var failure struct {
				Error struct {
					Code string `json:"code"`
					Type string `json:"type"`
				} `json:"error"`
			}
			// Interpret only known error identifiers; never expose provider messages.
			if json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&failure) == nil {
				if failure.Error.Code == "insufficient_quota" || failure.Error.Type == "insufficient_quota" {
					return nil, errors.New("generation API quota exhausted (HTTP 429)")
				}
				if failure.Error.Code == "rate_limit_exceeded" || failure.Error.Type == "rate_limit_exceeded" {
					return nil, errors.New("generation rate limit exceeded (HTTP 429); wait before resuming")
				}
			}
			return nil, errors.New("generation quota or rate limit exceeded (HTTP 429)")
		}
		if response.StatusCode == 401 {
			return nil, errors.New("generation credential rejected (HTTP 401)")
		}
		if response.StatusCode == 400 {
			return nil, errors.New("generation request or model configuration rejected (HTTP 400)")
		}
		if response.StatusCode >= 500 {
			return nil, fmt.Errorf("generation provider temporarily unavailable (HTTP %d)", response.StatusCode)
		}
		return nil, errors.New("generation provider rejected request; check model, credentials and quota")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("generation response too large")
	}
	var envelope struct {
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Choices []struct {
			Finish  string `json:"finish_reason"`
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return nil, errors.New("invalid generation response")
	}
	text := ""
	if a.Kind == "cloud" {
		if envelope.Status != "completed" {
			return nil, errors.New("generation incomplete or refused")
		}
		for _, item := range envelope.Output {
			if item.Type == "message" {
				for _, c := range item.Content {
					if c.Type == "refusal" {
						return nil, errors.New("generation refused")
					}
					if c.Type == "output_text" {
						text += c.Text
					}
				}
			}
		}
	} else {
		if len(envelope.Choices) != 1 || envelope.Choices[0].Finish != "stop" || envelope.Choices[0].Message.Refusal != "" {
			return nil, errors.New("generation incomplete or refused")
		}
		text = envelope.Choices[0].Message.Content
	}
	if len(text) > 100000 || !json.Valid([]byte(text)) {
		return nil, errors.New("generation violated JSON contract")
	}
	if _, err = privacy.Clear("generation-output", []byte(text), nil); err != nil {
		return nil, errors.New("generation output failed privacy scan")
	}
	return []byte(text), nil
}
