package privacy

import (
	"errors"
	"testing"
)

func TestModelEgressPolicy(t *testing.T) {
	cases := []struct {
		name    string
		request ModelRequest
		allowed bool
	}{
		{"mock with cleared input", ModelRequest{Mode: "strict_local", ProviderKind: "mock", Sanitized: true, ScanSucceeded: true}, true},
		{"cloud denied in strict local", ModelRequest{Mode: "strict_local", ProviderKind: "cloud", ProviderURL: "https://api.openai.com/v1/responses", RepoOptedIn: true, Sanitized: true, ScanSucceeded: true}, false},
		{"cloud requires repo opt in", ModelRequest{Mode: "cloud_opt_in", ProviderKind: "cloud", ProviderURL: "https://api.openai.com/v1/responses", Sanitized: true, ScanSucceeded: true}, false},
		{"cloud approved", ModelRequest{Mode: "cloud_opt_in", ProviderKind: "cloud", ProviderURL: "https://api.openai.com/v1/responses", RepoOptedIn: true, Sanitized: true, ScanSucceeded: true}, true},
		{"scanner error blocks request", ModelRequest{Mode: "cloud_opt_in", ProviderKind: "cloud", ProviderURL: "https://api.openai.com/v1/responses", RepoOptedIn: true, Sanitized: true}, false},
		{"local network host rejected", ModelRequest{Mode: "strict_local", ProviderKind: "local", ProviderURL: "http://192.168.1.2:11434", Sanitized: true, ScanSucceeded: true}, false},
		{"loopback local accepted", ModelRequest{Mode: "strict_local", ProviderKind: "local", ProviderURL: "http://127.0.0.1:11434", Sanitized: true, ScanSucceeded: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Authorize(tc.request)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed = %v, want %v", err == nil, tc.allowed)
			}
		})
	}
}

func TestScannerFailureAndSecretExclusion(t *testing.T) {
	if _, err := Clear("src/handler.go", []byte("safe code"), errors.New("scanner unavailable")); !errors.Is(err, ErrScanFailed) {
		t.Fatalf("scanner failure must block content: %v", err)
	}
	if _, err := Clear("deploy/.env.synthetic", []byte("SYNTHETIC_CANARY_NOT_A_CREDENTIAL"), nil); !errors.Is(err, ErrExcluded) {
		t.Fatalf("secret fixture must be excluded: %v", err)
	}
}
