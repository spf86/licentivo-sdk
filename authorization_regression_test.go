package licentivo

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf86/licentivo-sdk/internal/lease"
)

func authorizationTestClient(t *testing.T, policy lease.RuntimePolicy, ttl int64, deny *atomic.Bool, code string) *Client {
	t.Helper()
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	keyID, product := "11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333"
	device, err := MachineDeviceID(product)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if deny != nil && deny.Load() {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": "authorization denied"}, "request_id": "audit"})
			return
		}
		now := time.Now().Unix()
		envelope, err := lease.Sign(lease.Claims{Version: 1, KeyID: keyID, TenantID: "22222222-2222-4222-8222-222222222222", ProductID: product, LicenseID: "44444444-4444-4444-8444-444444444444", DeviceID: device, ActivationID: "55555555-5555-4555-8555-555555555555", Mode: "perpetual", Features: []string{"export"}, MaxDevices: 2, IssuedAt: now, ExpiresAt: now + ttl, RuntimePolicy: &policy}, key)
		if err != nil {
			t.Error(err)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": response{DeviceKey: "lv_dev_" + strings.Repeat("d", 43), ActivationID: "55555555-5555-4555-8555-555555555555", Lease: &envelope, Heartbeat: policy.HeartbeatSeconds}, "request_id": "audit"})
	}))
	t.Cleanup(server.Close)
	c, err := New(Config{BaseURL: server.URL, ProductID: product, LicenseKey: "lv_lic_" + strings.Repeat("a", 43), TrustedKeys: map[string]string{keyID: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}, AllowHTTP: true, CachePath: filepath.Join(t.TempDir(), "lease.json")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if _, err := c.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMeteringAuthorizationDenialInvalidatesCachedGrant(t *testing.T) {
	for _, operation := range []string{"consume", "reserve"} {
		for _, code := range []string{"PRODUCT_DISABLED", "TENANT_SUSPENDED", "VERSION_NOT_ENTITLED", "LICENSE_REVOKED", "FEATURE_QUOTA_EXCEEDED"} {
			t.Run(operation+"/"+code, func(t *testing.T) {
				var deny atomic.Bool
				c := authorizationTestClient(t, lease.RuntimePolicy{OfflineMode: "limited", OfflineSeconds: 3600, HeartbeatSeconds: 600}, 3600, &deny, code)
				deny.Store(true)
				var err error
				if operation == "consume" {
					_, err = c.Consume(context.Background(), "export", 1, "audit-operation")
				} else {
					_, err = c.Reserve(context.Background(), "export", 1, "audit-operation")
				}
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Code != code {
					t.Fatalf("expected server denial %s, got %v", code, err)
				}
				allowed := c.CheckFeature("export")
				t.Logf("server denial=%s, subsequent local feature allowed=%t, status=%+v", code, allowed, c.Status())
				if code == "FEATURE_QUOTA_EXCEEDED" {
					if !allowed {
						t.Fatal("quota failure should retain other feature authorization")
					}
				} else if allowed {
					t.Fatalf("explicit authorization denial %s left the cached grant usable", code)
				} else if code != "LICENSE_REVOKED" {
					if c.auth.DeviceKey == "" {
						t.Fatal("recoverable denial discarded machine credential")
					}
					deny.Store(false)
					if _, err := c.RefreshOnline(context.Background()); err != nil || !c.CheckFeature("export") {
						t.Fatal("authorization could not recover after the server resumed it", err)
					}
				}
			})
		}
	}
}

func TestHeartbeatRunsBeforeSignedDeadline(t *testing.T) {
	c := authorizationTestClient(t, lease.RuntimePolicy{OfflineMode: "limited", OfflineSeconds: 600, HeartbeatSeconds: 600}, 600, nil, "")
	delays := make(chan time.Duration, 1)
	c.waitHeartbeat = func(_ context.Context, delay time.Duration) bool { delays <- delay; return false }
	if err := c.StartHeartbeat(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	select {
	case delay := <-delays:
		claims, err := c.localClaims()
		if err != nil {
			t.Fatal(err)
		}
		remaining := time.Until(time.Unix(claims.ExpiresAt, 0))
		if delay >= remaining {
			t.Fatalf("healthy online client schedules heartbeat after its signed authorization expires: delay=%v, remaining=%v", delay, remaining)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat did not start")
	}
}

func TestExplicitLoginAfterLogoutDiscardsOldAuthorization(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	c := authorizationTestClient(t, lease.RuntimePolicy{OfflineMode: "limited", OfflineSeconds: 3600, HeartbeatSeconds: 600}, 3600, nil, "")
	cfg := c.cfg
	if _, err := c.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.CheckFeature("export") || reopened.auth.DeviceKey != "" {
		t.Fatal("explicit login restored the signed-out grant or revoked credential")
	}
	if _, err := reopened.Activate(context.Background()); err != nil || !reopened.CheckFeature("export") {
		t.Fatal("explicit license login could not obtain fresh online authorization", err)
	}
}
