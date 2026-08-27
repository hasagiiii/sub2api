package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// PprofServer serves Go's standard profiling endpoints on a dedicated
// listener. Keeping this separate from the public API listener makes it
// possible to expose profiling only on a private diagnostics interface.
type PprofServer struct {
	config   config.PprofConfig
	server   *http.Server
	listener net.Listener
}

// NewPprofServer creates an optional pprof server from configuration. The
// listener is opened by Start so configuration errors can be reported before
// the process begins serving traffic.
func NewPprofServer(cfg config.PprofConfig) *PprofServer {
	return &PprofServer{config: cfg}
}

// Handler returns a fresh mux containing all standard pprof endpoints.
// It is exported to make embedding the diagnostics handler in tests or a
// separately managed listener straightforward.
func (s *PprofServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	for _, name := range []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"} {
		mux.Handle("/debug/pprof/"+name, pprof.Handler(name))
	}
	return mux
}

// Address returns the configured listen address.
func (s *PprofServer) Address() string {
	host := strings.TrimSpace(s.config.Host)
	return net.JoinHostPort(host, strconv.Itoa(s.config.Port))
}

// Start opens the pprof listener. It is a no-op when profiling is disabled.
func (s *PprofServer) Start() error {
	if s == nil || !s.config.Enabled {
		return nil
	}
	if s.listener != nil {
		return errors.New("pprof server already started")
	}
	listener, err := net.Listen("tcp", s.Address())
	if err != nil {
		return fmt.Errorf("listen for pprof on %s: %w", s.Address(), err)
	}
	s.listener = listener
	s.server = &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("pprof server stopped unexpectedly: %v", err)
		}
	}()
	log.Printf("pprof server listening on %s", listener.Addr().String())
	return nil
}

// Shutdown gracefully stops the diagnostics listener. It is safe to call for
// a disabled or never-started server.
func (s *PprofServer) Shutdown(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	err := s.server.Shutdown(ctx)
	s.server = nil
	s.listener = nil
	return err
}
