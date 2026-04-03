package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"claudecodeproxy/internal/claude"
)

type Server struct {
	host   string
	port   int
	runner claude.Runner
}

func New(host string, port int, maxConcurrent int) *Server {
	return &Server{host: host, port: port, runner: claude.NewCLIRunner(maxConcurrent)}
}

// NewWithRunner creates a server with a custom CLI runner (for testing).
func NewWithRunner(host string, port int, runner claude.Runner) *Server {
	return &Server{host: host, port: port, runner: runner}
}

// Handler returns the HTTP handler for use in tests.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", s.MessagesHandler)
	mux.HandleFunc("GET /health", s.HealthHandler)
	return mux
}

func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	srv := &http.Server{
		Addr:    addr,
		Handler: withLogging(s.Handler()),
	}

	// Graceful shutdown on signal
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on %s", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// StartOnListener starts the server on a pre-existing listener (for tests).
func (s *Server) StartOnListener(listener net.Listener) error {
	srv := &http.Server{Handler: s.Handler()}
	return srv.Serve(listener)
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start))
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Unwrap allows the http.Flusher interface to pass through.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}
