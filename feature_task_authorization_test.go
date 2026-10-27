package licentivo

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf86/licentivo-sdk/internal/lease"
)

func TestMeteredFeatureChecksAuthorizationAfterReservation(t *testing.T) {
	for _, scenario := range []string{"reservation_expired", "expiry_missing", "lease_expired", "valid"} {
		t.Run(scenario, func(t *testing.T) {
			private := ed25519.NewKeyFromSeed(make([]byte, 32))
			keyID, product, activation := "11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333", "55555555-5555-4555-8555-555555555555"
			device, err := MachineDeviceID(product)
			if err != nil {
				t.Fatal(err)
			}
			var deadline atomic.Int64
			activations, commits := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/api/v1/consume/") {
					status, expiry := "pending", time.Now().UTC().Add(time.Hour)
					switch strings.TrimPrefix(r.URL.Path, "/api/v1/consume/") {
					case "reserve":
						switch scenario {
						case "reservation_expired":
							expiry = time.Now().UTC().Add(-time.Minute)
						case "expiry_missing":
							expiry = time.Time{}
						case "lease_expired":
							timer := time.NewTimer(time.Until(time.Unix(deadline.Load(), 0).Add(20 * time.Millisecond)))
							defer timer.Stop()
							<-timer.C
						}
					case "commit":
						commits++
						status = "committed"
					case "cancel":
						status = "canceled"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": response{ActivationID: activation, Reservation: &Reservation{ID: "66666666-6666-4666-8666-666666666666", OperationID: r.Header.Get("Idempotency-Key"), Status: status, ExpiresAt: expiry}}, "request_id": "local-test"})
					return
				}
				activations++
				now := time.Now().Unix()
				expires := now + 3600
				if scenario == "lease_expired" && activations == 1 {
					expires = now + 2
					deadline.Store(expires)
				}
				signed, _ := lease.Sign(lease.Claims{Version: 1, KeyID: keyID, TenantID: "22222222-2222-4222-8222-222222222222", ProductID: product, LicenseID: "44444444-4444-4444-8444-444444444444", DeviceID: device, ActivationID: activation, Mode: "timed", Features: []string{"export"}, MaxDevices: 1, IssuedAt: now, ExpiresAt: expires, RuntimePolicy: &lease.RuntimePolicy{OfflineMode: "limited", OfflineSeconds: 3600}}, private)
				_ = json.NewEncoder(w).Encode(map[string]any{"data": response{ActivationID: activation, Lease: &signed, DeviceKey: "lv_dev_" + strings.Repeat("d", 43)}, "request_id": "local-test"})
			}))
			defer server.Close()
			cfg := Config{BaseURL: server.URL, ProductID: product, LicenseKey: "lv_lic_" + strings.Repeat("a", 43), TrustedKeys: map[string]string{keyID: base64.RawURLEncoding.EncodeToString(private.Public().(ed25519.PublicKey))}, AllowHTTP: true, CachePath: filepath.Join(t.TempDir(), "lease.json")}
			client, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Activate(context.Background()); err != nil {
				t.Fatal(err)
			}
			calls := 0
			err = client.RunMeteredFeature(context.Background(), "export", 1, "boundary-job", func() error { calls++; return nil })
			valid := scenario == "valid"
			if valid && (err != nil || calls != 1 || commits != 1) || !valid && (err == nil || calls != 0 || commits != 0) {
				t.Fatalf("SDK ran business beyond its authorization boundary: calls=%d, commits=%d, error=%v", calls, commits, err)
			}
			client.Close()
			cfg.LicenseKey = ""
			reopened, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if _, err := reopened.Activate(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := reopened.ResolveMeteredFeature(context.Background(), "boundary-job", valid); err != nil {
				t.Fatal("restart could not settle the recorded outcome", err)
			}
			err = reopened.RunMeteredFeature(context.Background(), "export", 1, "boundary-job", func() error { calls++; return nil })
			if valid && (err != nil || calls != 1) || !valid && (err == nil || calls != 0) {
				t.Fatal("recovery repeated business or changed its outcome", calls, err)
			}
		})
	}
}
