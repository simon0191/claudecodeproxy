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
	"claudecodeproxy/internal/proxy"
)

type Server struct {
	host            string
	port            int
	messagesHandler http.HandlerFunc
}

// NewCLI creates a server in CLI mode (shells out to claude).
func NewCLI(host string, port int, maxConcurrent int) *Server {
	runner := claude.NewCLIRunner(maxConcurrent)
	return NewCLIWithRunner(host, port, runner)
}

// NewCLIWithRunner creates a CLI-mode server with a custom runner (for testing).
func NewCLIWithRunner(host string, port int, runner claude.Runner) *Server {
	s := &Server{host: host, port: port}
	s.messagesHandler = s.makeCLIHandler(runner)
	return s
}

// NewPassthrough creates a server that forwards requests to the Anthropic API.
func NewPassthrough(host string, port int, auth proxy.AuthConfig, baseURL string) *Server {
	p := proxy.NewPassthrough(auth, baseURL)
	return &Server{host: host, port: port, messagesHandler: p.Handle}
}

// NewAugmented creates a server that injects beta headers and forwards to the API.
func NewAugmented(host string, port int, auth proxy.AuthConfig, baseURL string) *Server {
	p := proxy.NewAugmented(auth, baseURL)
	return &Server{host: host, port: port, messagesHandler: p.Handle}
}

// Handler returns the HTTP handler for use in tests.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", s.messagesHandler)
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
