package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0merUfuk/skuggsja/internal/analytics"
)

func TestHandlerServesEmbeddedAssetsWithStrictHeaders(t *testing.T) {
	t.Parallel()

	handler := Handler(analytics.Report{SchemaVersion: 1, ProductName: "skuggsja"})
	for _, path := range []string{"/", "/styles.css", "/app.js", "/api/rewind", "/healthz"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Host = "127.0.0.1:4321"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Errorf("GET %s returned %d", path, response.Code)
		}
		csp := response.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "connect-src 'self'") {
			t.Errorf("GET %s has unexpected CSP %q", path, csp)
		}
		if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("GET %s Referrer-Policy = %q", path, got)
		}
	}
}

func TestHandlerRejectsNonLoopbackHostHeader(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "http://attacker.example/api/rewind", nil)
	response := httptest.NewRecorder()
	Handler(analytics.Report{}).ServeHTTP(response, request)
	if response.Code != http.StatusMisdirectedRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMisdirectedRequest)
	}
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatal("rejected response is missing security headers")
	}
}

func TestListenLoopbackNeverBindsLAN(t *testing.T) {
	t.Parallel()

	listener, err := listenLoopback(0)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !address.IP.IsLoopback() {
		t.Fatalf("listener address %v is not loopback", listener.Addr())
	}
}

func TestListenLoopbackFallsBackWhenPreferredPortIsBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	preferred := busy.Addr().(*net.TCPAddr).Port

	listener, err := listenLoopback(preferred)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	got := listener.Addr().(*net.TCPAddr)
	if !got.IP.IsLoopback() {
		t.Fatalf("fallback address %v is not loopback", got)
	}
	if got.Port == preferred {
		t.Fatalf("fallback reused occupied port %d", preferred)
	}
}
