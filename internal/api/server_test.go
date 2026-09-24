package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"onboardmeplease/internal/config"
)

func TestLocalSessionAndOriginBoundary(t *testing.T) {
	server := New(nil, nil, config.Config{ListenAddr: "127.0.0.1:8765", PublicHost: "127.0.0.1:8765"})
	handler := server.Handler()
	request := func(method, path, host string) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		r.Host = host
		return r
	}
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, request("GET", "/v1/session", "attacker.example"))
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("unexpected host status = %d", blocked.Code)
	}
	blocked = httptest.NewRecorder()
	crossSite := request("GET", "/v1/session", "127.0.0.1:8765")
	crossSite.Header.Set("Origin", "https://attacker.example")
	handler.ServeHTTP(blocked, crossSite)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", blocked.Code)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request("GET", "/v1/session", "127.0.0.1:8765"))
	if response.Code != http.StatusOK || len(response.Result().Cookies()) != 1 {
		t.Fatalf("session status = %d, cookies = %d", response.Code, len(response.Result().Cookies()))
	}
	if !strings.Contains(response.Header().Get("Set-Cookie"), "SameSite=Strict") {
		t.Fatal("local session cookie is missing SameSite=Strict")
	}
	noCSRF := httptest.NewRecorder()
	write := request("POST", "/v1/repositories", "127.0.0.1:8765")
	write.AddCookie(response.Result().Cookies()[0])
	handler.ServeHTTP(noCSRF, write)
	if noCSRF.Code != http.StatusForbidden {
		t.Fatalf("write without CSRF status = %d", noCSRF.Code)
	}
}

func TestLocalRepositoryRequestsAreRejectedBeforeDatabaseAccess(t *testing.T) {
	server := New(nil, nil, config.Config{ListenAddr: "127.0.0.1:8765", PublicHost: "127.0.0.1:8765"})
	handler := server.Handler()
	sessionRequest := httptest.NewRequest(http.MethodGet, "/v1/session", nil)
	sessionRequest.Host = "127.0.0.1:8765"
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, sessionRequest)
	var sessionBody struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(sessionResponse.Body.Bytes(), &sessionBody); err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{
		`{"kind":"local","path":"C:/dev/project","snapshot_choice":"working_tree","privacy_mode":"strict_local"}`,
		`{"kind":"local","url":"https://github.com/example/project","privacy_mode":"strict_local"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/v1/repositories", strings.NewReader(payload))
		request.Host = "127.0.0.1:8765"
		request.AddCookie(sessionResponse.Result().Cookies()[0])
		request.Header.Set("X-CSRF-Token", sessionBody.CSRF)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("local request status = %d: %s", response.Code, response.Body.String())
		}
	}
}
