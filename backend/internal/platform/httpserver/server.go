package httpserver

import (
	"log/slog"
	"net/http"
	"time"
)

const maxHeaderBytes = 1 << 20

// RouteRegistrar contributes an explicitly composed set of HTTP routes.
type RouteRegistrar interface {
	Register(*http.ServeMux)
}

// New returns an HTTP server with conservative resource limits.
func New(address string, logger *slog.Logger, registrars ...RouteRegistrar) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           newHandler(registrars...),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

func newHandler(registrars ...RouteRegistrar) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	for _, registrar := range registrars {
		registrar.Register(mux)
	}
	return mux
}

func health(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(`{"status":"ok"}`))
}
