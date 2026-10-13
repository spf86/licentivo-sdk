package licentivo

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHeartbeatDelayMinimum(t *testing.T) {
	for _, interval := range []int{10, 300, 600, 3600, 86400} {
		for jitter := 0; jitter < 256; jitter++ {
			delay := heartbeatDelay(interval, byte(jitter))
			if delay < 600*time.Second || delay < time.Duration(interval)*time.Second || delay > time.Duration(float64(max(600, interval))*1.1*float64(time.Second)) {
				t.Fatalf("unsafe schedule %d/%d: %v", interval, jitter, delay)
			}
		}
	}
}

func TestRetryAfterAndHTMLProxyFailure(t *testing.T) {
	for _, status := range []int{429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Retry-After", "45")
				w.Header().Set("X-Request-ID", "proxy-trace")
				w.WriteHeader(status)
				w.Write([]byte("<html>temporarily unavailable</html>"))
			}))
			defer server.Close()
			c := &Client{cfg: Config{BaseURL: server.URL, LicenseKey: "lv_lic_" + strings.Repeat("a", 43), CachePath: filepath.Join(t.TempDir(), "lease.json")}, http: server.Client()}
			_, transient, err := c.request(context.Background(), "validate", "activation")
			e, ok := err.(*APIError)
			if !ok || !transient || !e.Retryable || e.Status != status || e.RetryAfterSeconds != 45 || e.RequestID != "proxy-trace" {
				t.Fatalf("lost structured proxy failure: %v", err)
			}
			want := 2
			if status == 429 {
				want = 1
			}
			if requests != want {
				t.Fatalf("requests=%d, want %d", requests, want)
			}
		})
	}
}

func TestStartupRefreshCanBeCanceledWithoutNetworkMutex(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the request so net/http can observe the client's cancellation.
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	c := &Client{cfg: Config{BaseURL: server.URL, LicenseKey: "lv_lic_" + strings.Repeat("a", 43), CachePath: filepath.Join(t.TempDir(), "lease.json")}, http: server.Client()}
	c.startRefresh(context.Background(), nil)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	start := time.Now()
	c.stopRefresh()
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("shutdown waited for HTTP timeout")
	}
}
