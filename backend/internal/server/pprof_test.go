package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestPprofHandlerExposesStandardEndpoints(t *testing.T) {
	h := NewPprofServer(config.PprofConfig{}).Handler()

	for _, path := range []string{"/debug/pprof/", "/debug/pprof/heap", "/debug/pprof/goroutine"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("GET %s returned %d, want %d", path, resp.Code, http.StatusOK)
		}
	}
}

func TestPprofServerDisabledIsNoOp(t *testing.T) {
	s := NewPprofServer(config.PprofConfig{Host: "127.0.0.1", Port: 1})
	if err := s.Start(); err != nil {
		t.Fatalf("Start() on disabled server returned error: %v", err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() on disabled server returned error: %v", err)
	}
}

func TestPprofServerStartsAndShutsDown(t *testing.T) {
	s := NewPprofServer(config.PprofConfig{Enabled: true, Host: "127.0.0.1", Port: 0})
	if err := s.Start(); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skip("network listeners are not permitted in this environment")
		}
		t.Fatalf("Start() returned error: %v", err)
	}
	defer func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Errorf("deferred Shutdown() returned error: %v", err)
		}
	}()

	// Port 0 asks the OS for an ephemeral port, which keeps this test isolated.
	address := s.listener.Addr().String()
	var body string
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		resp, err := http.Get("http://" + address + "/debug/pprof/")
		if err == nil {
			body = mustReadAndClose(t, resp)
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(body, "profile") {
		t.Fatalf("pprof index response did not contain profile link: %q", body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() returned error: %v", err)
	}
}

func mustReadAndClose(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()
	var b strings.Builder
	if _, err := io.Copy(&b, resp.Body); err != nil {
		t.Fatalf("read response: %v", err)
	}
	return b.String()
}
