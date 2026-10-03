package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type Server struct {
	httpServer *http.Server

	mu       sync.Mutex
	listener net.Listener
}

func New(handler http.Handler) (*Server, error) {
	if handler == nil {
		return nil, errors.New("http handler is required")
	}

	return &Server{
		httpServer: &http.Server{
			Addr:              address(),
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
	}, nil
}

func (s *Server) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("start context is required")
	}

	listener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP on %s: %w", s.httpServer.Addr, err)
	}

	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()

	go func() {
		err := s.httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "HTTP server stopped unexpectedly: %v\n", err)
		}
	}()

	fmt.Printf("HTTP API listening on %s\n", listener.Addr().String())
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.New("stop context is required")
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}

	return nil
}

func address() string {
	port := strings.TrimSpace(os.Getenv("HTTP_PORT"))
	if port == "" {
		port = "8080"
	}
	return ":" + port
}
