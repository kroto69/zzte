package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"olt-monitor/internal/config"
)

// Server holds the HTTP server
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
}

// NewServer creates a new HTTP server
func NewServer(cfg *config.Config, handler http.Handler) *Server {
	return &Server{
		cfg: cfg,
		httpServer: &http.Server{
			Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
			Handler:      handler,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
