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
	"testing"
	"time"

	"github.com/spf86/licentivo-sdk/internal/lease"
)

func TestMeteredFeatureExpiredSettlementRecovery(t *testing.T) {
	for _, scenario := range []string{"canceled", "expired", "cancel_unavailable_then_expired", "commit_expired"} {
		t.Run(scenario, func(t *testing.T) {
			private := ed25519.NewKeyFromSeed(make([]byte, 32))
			keyID, product, activation := "11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333", "55555555-5555-4555-8555-555555555555"
			device, err := MachineDeviceID(product)
			if err != nil {
				t.Fatal(err)
			}
			reserves, cancels, commits := 0, 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/api/v1/consume/") {
					status := "pending"
					switch strings.TrimPrefix(r.URL.Path, "/api/v1/consume/") {
					case "reserve":
						reserves++
					case "cancel":
						cancels++
						if scenario == "cancel_unavailable_then_expired" && cancels == 1 {
							w.WriteHeader(http.StatusServiceUnavailable)
							_, _ = w.Write([]byte(`{"error":{"code":"TEMPORARILY_UNAVAILABLE","message":"retry cancellation"}}`))
							return
						}
						status = "expired"
						if scenario == "canceled" {
							status = "canceled"
						}
					case "commit":
						commits++
						w.WriteHeader(http.StatusConflict)
						_, _ = w.Write([]byte(`{"error":{"code":"RESERVATION_EXPIRED","message":"do not repeat completed business"}}`))
						return
					}
					// The hold is valid when business starts. Settlement above
					// simulates its expiry while that business was running.
					_ = json.NewEncoder(w).Encode(map[string]any{"data": response{ActivationID: activation, Reservation: &Reservation{ID: "66666666-6666-4666-8666-666666666666", OperationID: r.Header.Get("Idempotency-Key"), Status: status, ExpiresAt: time.Now().UTC().Add(time.Minute)}}, "request_id": "local-test"})
					return
				}
				now := time.Now().Unix()
				signed, _ := lease.Sign(lease.Claims{Version: 1, KeyID: keyID, TenantID: "22222222-2222-4222-8222-222222222222", ProductID: product, LicenseID: "44444444-4444-4444-8444-444444444444", DeviceID: device, ActivationID: activation, Mode: "timed", Features: []string{"export"}, MaxDevices: 1, IssuedAt: now, ExpiresAt: now + 3600, RuntimePolicy: &lease.RuntimePolicy{OfflineMode: "limited", OfflineSeconds: 3600}}, private)
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
			businessErr := errors.New("business failed before producing a result")
			succeeded := scenario == "commit_expired"
			err = client.RunMeteredFeature(context.Background(), "export", 1, "expired-job", func() error {
				calls++
				if succeeded {
					return nil
				}
				return businessErr
			})
			if err == nil || !succeeded && !errors.Is(err, businessErr) || calls != 1 {
				t.Fatal("business result was lost", err, calls)
			}
			path, err := client.taskPath("expired-job")
			if err != nil {
				t.Fatal(err)
			}
			client.Close()
			cfg.LicenseKey = ""
			reopened, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			for range 2 {
				err = reopened.ResolveMeteredFeature(context.Background(), "expired-job", succeeded)
				if succeeded && err == nil || !succeeded && err != nil {
					t.Fatal("expired settlement did not preserve the business outcome", err)
				}
			}
			task, err := readFeatureTask(path, "expired-job")
			want := "canceled"
			if succeeded {
				want = "commit_pending"
			}
			if err != nil || task.State != want {
				t.Fatal("settlement was not persisted", task, err)
			}
			if err := reopened.ResolveMeteredFeature(context.Background(), "expired-job", !succeeded); err == nil {
				t.Fatal("business outcome was flipped")
			}
			if err := reopened.RunMeteredFeature(context.Background(), "export", 1, "expired-job", func() error { calls++; return nil }); err == nil || calls != 1 || reserves != 1 {
				t.Fatal("expired operation repeated business", err, calls, reserves)
			}
			wantCancels := 1
			if scenario == "cancel_unavailable_then_expired" {
				wantCancels = 2
			}
			if succeeded {
				wantCancels = 0
			}
			if cancels != wantCancels || !succeeded && commits != 0 {
				t.Fatal("settled cancellation kept contacting the server", cancels, commits)
			}
		})
	}
}
