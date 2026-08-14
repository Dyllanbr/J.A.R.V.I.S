package httpserver

import (
	"log/slog"
	"net/http"
	"time"
)

const maxHeaderBytes = 1 << 20

// New returns an HTTP server with conservative resource limits.
func New(address string, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           newHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	return mux
}

func health(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(`{"status":"ok"}`))
}
