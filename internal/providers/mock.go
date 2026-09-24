package providers

import (
	"context"
	"crypto/sha256"
	"errors"

	"onboardmeplease/internal/privacy"
)

type Model interface {
	Generate(context.Context, privacy.ModelRequest, string) (string, error)
	Embed(context.Context, privacy.ModelRequest, string) ([]float32, error)
}

// Mock is deterministic and intentionally does not pretend to understand code.
type Mock struct{}

func (Mock) Generate(_ context.Context, request privacy.ModelRequest, _ string) (string, error) {
	request.ProviderKind = "mock"
	if err := privacy.Authorize(request); err != nil {
		return "", err
	}
	return "Mock model response; no repository analysis was performed.", nil
}

func (Mock) Embed(_ context.Context, request privacy.ModelRequest, text string) ([]float32, error) {
	request.ProviderKind = "mock"
	if err := privacy.Authorize(request); err != nil {
		return nil, err
	}
	if text == "" {
		return nil, errors.New("cannot embed empty text")
	}
	sum := sha256.Sum256([]byte(text))
	return []float32{float32(sum[0]) / 255, float32(sum[1]) / 255, float32(sum[2]) / 255, float32(sum[3]) / 255}, nil
}
