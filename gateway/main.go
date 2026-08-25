package main

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type config struct {
	listen             string
	orchestratorSocket string
	staticDir          string
	identityHeader     string
	publicOrigin       string
}

func main() {
	cfg := loadConfig()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := validateConfig(cfg); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("GET /api/terminal", func(w http.ResponseWriter, r *http.Request) {
		serveTerminal(w, r, cfg, logger)
	})

	static := http.FileServer(http.Dir(cfg.staticDir))
	mux.Handle("/", spaHandler(cfg.staticDir, static))

	server := &http.Server{
		Addr:              cfg.listen,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	logger.Info("gateway listening", "address", cfg.listen)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("gateway stopped", "error", err)
		os.Exit(1)
	}
}

func loadConfig() config {
	return config{
		listen:             env("WEBNIX_LISTEN", "127.0.0.1:8080"),
		orchestratorSocket: env("WEBNIX_ORCHESTRATOR_SOCKET", "/run/webnix/orchestrator.sock"),
		staticDir:          env("WEBNIX_STATIC_DIR", "./frontend/dist"),
		identityHeader:     env("WEBNIX_IDENTITY_HEADER", "X-Auth-Request-Email"),
		publicOrigin:       strings.TrimRight(os.Getenv("WEBNIX_PUBLIC_ORIGIN"), "/"),
	}
}

func serveTerminal(w http.ResponseWriter, r *http.Request, cfg config, logger *slog.Logger) {
	principal, ok := principalFromRequest(r, cfg.identityHeader)
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if cfg.publicOrigin == "" {
		http.Error(w, "WEBNIX_PUBLIC_ORIGIN is not configured", http.StatusServiceUnavailable)
		return
	}
	if origin := strings.TrimRight(r.Header.Get("Origin"), "/"); origin != cfg.publicOrigin {
		http.Error(w, "invalid websocket origin", http.StatusForbidden)
		return
	}

	tenant := tenantID(principal)
	client := unixHTTPClient(cfg.orchestratorSocket)
	orchestratorURL := "ws://webnix.local/v1/terminal?tenant=" + tenant

	upstream, _, err := websocket.Dial(r.Context(), orchestratorURL, &websocket.DialOptions{
		HTTPClient: client,
	})
	if err != nil {
		logger.Error("orchestrator connection failed", "tenant", tenant, "error", err)
		http.Error(w, "workspace service unavailable", http.StatusServiceUnavailable)
		return
	}
	defer upstream.CloseNow()

	downstream, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:  []string{strings.TrimPrefix(strings.TrimPrefix(cfg.publicOrigin, "https://"), "http://")},
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer downstream.CloseNow()
	downstream.SetReadLimit(128 << 10)
	upstream.SetReadLimit(128 << 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errC := make(chan error, 2)
	go proxyWS(ctx, downstream, upstream, errC)
	go proxyWS(ctx, upstream, downstream, errC)
	<-errC
	cancel()
	_ = downstream.Close(websocket.StatusNormalClosure, "session ended")
	_ = upstream.Close(websocket.StatusNormalClosure, "session ended")
}

func proxyWS(ctx context.Context, src, dst *websocket.Conn, errC chan<- error) {
	for {
		messageType, data, err := src.Read(ctx)
		if err != nil {
			errC <- err
			return
		}
		if err := dst.Write(ctx, messageType, data); err != nil {
			errC <- err
			return
		}
	}
}

func principalFromRequest(r *http.Request, header string) (string, bool) {
	principal := strings.TrimSpace(strings.ToLower(r.Header.Get(header)))
	if principal == "" || len(principal) > 320 || strings.ContainsAny(principal, "\r\n\x00") {
		return "", false
	}
	return principal, true
}

func tenantID(principal string) string {
	sum := sha256.Sum256([]byte("webnix:v1:" + principal))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
	return strings.ToLower(encoded[:26])
}

func unixHTTPClient(socketPath string) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socketPath)
		},
		DisableCompression: true,
	}
	return &http.Client{Transport: transport, Timeout: 0}
}

func spaHandler(root string, static http.Handler) http.Handler {
	rootFS := os.DirFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "."
		}
		if name == "." {
			w.Header().Set("Cache-Control", "no-cache")
			static.ServeHTTP(w, r)
			return
		}
		if fs.ValidPath(name) {
			if info, err := fs.Stat(rootFS, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				static.ServeHTTP(w, r)
				return
			}
		}

		w.Header().Set("Cache-Control", "no-cache")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		static.ServeHTTP(w, r2)
	})
}

func validateConfig(cfg config) error {
	if cfg.publicOrigin == "" {
		return errors.New("WEBNIX_PUBLIC_ORIGIN is required")
	}
	u, err := url.Parse(cfg.publicOrigin)
	if err != nil || u.Host == "" {
		return errors.New("WEBNIX_PUBLIC_ORIGIN must be an absolute URL")
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("WEBNIX_PUBLIC_ORIGIN must not contain a path, query, or fragment")
	}
	host := u.Hostname()
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !(local && u.Scheme == "http") {
		return errors.New("WEBNIX_PUBLIC_ORIGIN must use HTTPS outside localhost")
	}
	if cfg.identityHeader == "" || strings.ContainsAny(cfg.identityHeader, "\r\n: \t") {
		return errors.New("WEBNIX_IDENTITY_HEADER is invalid")
	}
	info, err := os.Stat(cfg.staticDir)
	if err != nil || !info.IsDir() {
		return errors.New("WEBNIX_STATIC_DIR does not exist or is not a directory")
	}
	return nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
