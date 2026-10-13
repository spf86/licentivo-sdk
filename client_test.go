package licentivo

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"github.com/spf86/licentivo-sdk/internal/lease"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHeartbeatPermanentOfflineAndPolicyRestart(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	keyID, product := "11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333"
	device, err := MachineDeviceID(product)
	if err != nil {
		t.Fatal(err)
	}
	var heartbeats atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		interval := 600
		if strings.HasSuffix(r.URL.Path, "/heartbeat") {
			heartbeats.Add(1)
			interval = 0
		}
		now := time.Now().Unix()
		e, _ := lease.Sign(lease.Claims{Version: 1, KeyID: keyID, TenantID: "22222222-2222-4222-8222-222222222222", ProductID: product, LicenseID: "44444444-4444-4444-8444-444444444444", DeviceID: device, ActivationID: "55555555-5555-4555-8555-555555555555", Mode: "perpetual", Features: []string{"export"}, MaxDevices: 1, IssuedAt: now, ExpiresAt: 253402300799, RuntimePolicy: &lease.RuntimePolicy{OfflineMode: "permanent", HeartbeatSeconds: interval, OfflineSeconds: 0}}, key)
		json.NewEncoder(w).Encode(map[string]any{"data": response{DeviceKey: "lv_dev_" + strings.Repeat("d", 43), ActivationID: "55555555-5555-4555-8555-555555555555", Lease: &e, Heartbeat: interval}, "request_id": "timer"})
	}))
	defer server.Close()
	c, err := New(Config{BaseURL: server.URL, ProductID: product, LicenseKey: "lv_lic_" + strings.Repeat("a", 43), TrustedKeys: map[string]string{keyID: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}, AllowHTTP: true, CachePath: filepath.Join(t.TempDir(), "lease.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.waitHeartbeat = func(ctx context.Context, delay time.Duration) bool {
		if delay < 600*time.Second {
			t.Error("heartbeat below minimum")
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
			return true
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err = c.Activate(ctx); err != nil {
		t.Fatal(err)
	}
	for wanted := int32(1); wanted <= 2; wanted++ {
		if wanted == 2 {
			if _, err = c.RefreshOnline(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if err = c.StartHeartbeat(ctx, nil); err != nil {
			t.Fatal(err)
		}
		deadline := time.NewTimer(15 * time.Second)
		tick := time.NewTicker(20 * time.Millisecond)
		for heartbeats.Load() < wanted {
			select {
			case <-deadline.C:
				t.Fatal("permanent offline blocked periodic heartbeat or restart")
			case <-tick.C:
			}
		}
		tick.Stop()
		deadline.Stop()
		if !c.CheckFeature("export") {
			t.Fatal("successful policy update lost offline access")
		}
	}
}

func TestCacheFallbackDenialAndLicenseContext(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	keyID := "11111111-1111-4111-8111-111111111111"
	product := "33333333-3333-4333-8333-333333333333"
	dev, err := MachineDeviceID(product)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	envelope, _ := lease.Sign(lease.Claims{Version: 1, KeyID: keyID, TenantID: "22222222-2222-4222-8222-222222222222", ProductID: product, LicenseID: "44444444-4444-4444-8444-444444444444", DeviceID: dev, ActivationID: "55555555-5555-4555-8555-555555555555", Mode: "timed", Features: []string{"export"}, MaxDevices: 1, IssuedAt: now, ExpiresAt: now + 3600}, key)
	var mode atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 1:
			w.WriteHeader(503)
			w.Write([]byte(`{"error":{"code":"SERVER_UNAVAILABLE","message":"temporary"}}`))
		case 2:
			w.WriteHeader(403)
			w.Write([]byte(`{"error":{"code":"DEVICE_RELEASED","message":"released"},"request_id":"test"}`))
		default:
			json.NewEncoder(w).Encode(map[string]any{"data": response{DeviceKey: "lv_dev_" + strings.Repeat("d", 43), ActivationID: "55555555-5555-4555-8555-555555555555", Lease: &envelope, Heartbeat: 10}, "request_id": "test"})
		}
	}))
	defer server.Close()
	cfg := Config{BaseURL: server.URL, ProductID: product, LicenseKey: "lv_lic_" + strings.Repeat("a", 43), DeviceID: dev, TrustedKeys: map[string]string{keyID: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}, CachePath: filepath.Join(t.TempDir(), "lease.json"), AllowHTTP: true}
	c, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Activate(context.Background()); e != nil {
		t.Fatal(e)
	}
	c.Close()
	mode.Store(1)
	reopened, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if _, e = reopened.Validate(context.Background()); e != nil || !reopened.CheckFeature("export") {
		t.Fatal("offline cache unavailable", e)
	}
	other := cfg
	other.LicenseKey = "lv_lic_" + strings.Repeat("b", 43)
	foreign, e := New(other)
	if e != nil {
		t.Fatal(e)
	}
	if foreign.CheckFeature("export") {
		t.Fatal("cache followed changed license credential")
	}
	foreign.Close()
	reopened.lastWall = time.Now().Add(10 * time.Minute)
	reopened.lastTick = time.Now()
	reopened.publish(false, nil)
	if reopened.CheckFeature("export") {
		t.Fatal("clock rollback accepted")
	}
	reopened.lastWall = time.Now()
	reopened.publish(false, nil)
	mode.Store(2)
	if _, e = reopened.Validate(context.Background()); e == nil {
		t.Fatal("explicit denial accepted cached lease")
	}
	if reopened.CheckFeature("export") {
		t.Fatal("denial did not invalidate cache")
	}
}
