package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/0merUfuk/skuggsja/internal/analytics"
	webassets "github.com/0merUfuk/skuggsja/web"
)

const contentSecurityPolicy = "default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'none'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'"

// ServeOptions configures the loopback-only UI.
type ServeOptions struct {
	Port        int
	OpenBrowser bool
	Ready       func(url string)
}

// Serve runs until ctx is canceled. A busy requested port falls back to an
// ephemeral loopback port.
func Serve(ctx context.Context, report analytics.Report, options ServeOptions) error {
	listener, err := listenLoopback(options.Port)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{
		Handler:           Handler(report),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	url := "http://" + listener.Addr().String()
	if options.Ready != nil {
		options.Ready(url)
	}
	if options.OpenBrowser {
		_ = openBrowser(url)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("stop local server: %w", err)
		}
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve local Rewind: %w", err)
	}
}

// Handler serves embedded same-origin assets and a content-free aggregate API.
func Handler(report analytics.Report) http.Handler {
	fileServer := http.FileServer(http.FS(webassets.Files))
	payload, marshalErr := json.Marshal(report)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/rewind", func(w http.ResponseWriter, _ *http.Request) {
		if marshalErr != nil {
			http.Error(w, "aggregate report unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.Handle("GET /", fileServer)
	return securityHeaders(loopbackHostOnly(mux))
}

func loopbackHostOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostname := r.Host
		if host, _, err := net.SplitHostPort(r.Host); err == nil {
			hostname = host
		}
		hostname = strings.Trim(strings.ToLower(hostname), "[]")
		if hostname != "127.0.0.1" && hostname != "localhost" && hostname != "::1" {
			http.Error(w, "loopback host required", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func listenLoopback(port int) (net.Listener, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err == nil || port == 0 {
		if err != nil {
			return nil, fmt.Errorf("bind loopback server: %w", err)
		}
		return listener, nil
	}
	listener, fallbackErr := net.Listen("tcp", "127.0.0.1:0")
	if fallbackErr != nil {
		return nil, fmt.Errorf("bind loopback server after requested port failed: %w", errors.Join(err, fallbackErr))
	}
	return listener, nil
}

func openBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}
