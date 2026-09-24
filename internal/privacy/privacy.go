package privacy

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var ErrExcluded = errors.New("content excluded by privacy policy")
var ErrScanFailed = errors.New("content could not be cleared by privacy scan")
var ErrEgressDenied = errors.New("model egress denied by privacy policy")

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(?:api[_-]?key|secret|password|token)\s*[:=]\s*["']?[^\s"']{8,}`),
	regexp.MustCompile(`(?:ghp_|github_pat_)[A-Za-z0-9_]{16,}`),
	regexp.MustCompile(`SYNTHETIC_CANARY_NOT_A_CREDENTIAL`),
}

func ExcludedPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx") || strings.HasSuffix(base, ".key") {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		switch strings.ToLower(part) {
		case ".git", "node_modules", "vendor", "secrets", ".aws", ".ssh":
			return true
		}
	}
	return false
}

// Clear returns no content when a scanner is unavailable or reports a finding.
// This conservative scanner is a Phase 1 boundary, not exhaustive detection.
func Clear(path string, content []byte, scanErr error) ([]byte, error) {
	if ExcludedPath(path) {
		return nil, ErrExcluded
	}
	if scanErr != nil {
		return nil, ErrScanFailed
	}
	for _, pattern := range secretPatterns {
		if pattern.Match(content) {
			return nil, ErrExcluded
		}
	}
	return content, nil
}

type ModelRequest struct {
	Mode          string
	ProviderKind  string // mock, local, or cloud
	ProviderURL   string
	RepoOptedIn   bool
	ScanSucceeded bool
	Sanitized     bool
}

// Authorize is the only path through which future model adapters may send data.
func Authorize(request ModelRequest) error {
	if !request.ScanSucceeded || !request.Sanitized {
		return ErrEgressDenied
	}
	switch request.ProviderKind {
	case "mock":
		return nil
	case "local":
		parsed, err := url.Parse(request.ProviderURL)
		if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return ErrEgressDenied
		}
		ip := net.ParseIP(parsed.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return ErrEgressDenied
		}
		return nil
	case "cloud":
		if request.Mode != "cloud_opt_in" || !request.RepoOptedIn {
			return ErrEgressDenied
		}
		parsed, err := url.Parse(request.ProviderURL)
		if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() != "api.openai.com" {
			return ErrEgressDenied
		}
		return nil
	default:
		return ErrEgressDenied
	}
}
