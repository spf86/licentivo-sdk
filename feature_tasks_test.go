package licentivo

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf86/licentivo-sdk/internal/lease"
)

func TestMeteredCallbackCrashRecovery(t *testing.T) {
	private := ed25519.NewKeyFromSeed(make([]byte, 32))
	keyID, product, activation := "11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333", "55555555-5555-4555-8555-555555555555"
	device, err := MachineDeviceID(product)
	if err != nil {
		t.Fatal(err)
	}
	commits, cancels, reserves := 0, 0, 0
	blockJournal := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().Unix()
		if strings.HasPrefix(r.URL.Path, "/api/v1/consume/") {
			state := "pending"
			switch strings.TrimPrefix(r.URL.Path, "/api/v1/consume/") {
			case "reserve":
				reserves++
				if blockJournal != "" {
					_ = os.Mkdir(blockJournal, 0700)
				}
			case "commit":
				commits++
				state = "committed"
			case "cancel":
				cancels++
				state = "canceled"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": response{ActivationID: activation, Reservation: &Reservation{ID: "66666666-6666-4666-8666-666666666666", OperationID: r.Header.Get("Idempotency-Key"), Status: state, ExpiresAt: time.Now().UTC().Add(time.Hour)}}, "request_id": "local-test"})
			return
		}
		signed, _ := lease.Sign(lease.Claims{Version: 1, KeyID: keyID, TenantID: "22222222-2222-4222-8222-222222222222", ProductID: product, LicenseID: "44444444-4444-4444-8444-444444444444", DeviceID: device, ActivationID: activation, Mode: "timed", Features: []string{"export"}, MaxDevices: 1, IssuedAt: now, ExpiresAt: now + 3600, RuntimePolicy: &lease.RuntimePolicy{OfflineMode: "limited", OfflineSeconds: 3600}}, private)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": response{ActivationID: activation, Lease: &signed, DeviceKey: "lv_dev_" + strings.Repeat("d", 43)}, "request_id": "local-test"})
	}))
	defer server.Close()
	cfg := Config{BaseURL: server.URL, ProductID: product, LicenseKey: "lv_lic_" + strings.Repeat("a", 43), TrustedKeys: map[string]string{keyID: base64.RawURLEncoding.EncodeToString(private.Public().(ed25519.PublicKey))}, AllowHTTP: true, CachePath: filepath.Join(t.TempDir(), "lease.json")}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Activate(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err = c.RunFeature("not_entitled", func() error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatal("denied feature ran business")
	}
	result := filepath.Join(t.TempDir(), "durable-result.txt")
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected simulated crash")
			}
		}()
		_ = c.RunMeteredFeature(context.Background(), "export", 1, "crashed-job", func() error {
			calls++
			_ = os.WriteFile(result, []byte("finished"), 0600)
			panic("simulated process death after durable business result")
		})
	}()
	c.Close()
	cfg.LicenseKey = ""
	reopened, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.RunMeteredFeature(context.Background(), "export", 1, "crashed-job", func() error { calls++; return nil }); err == nil || !strings.Contains(err.Error(), "outcome unknown") || calls != 1 {
		t.Fatal("unknown outcome must not rerun business", err, calls)
	}
	if _, err = os.Stat(result); err != nil {
		t.Fatal(err)
	}
	if err = reopened.ResolveMeteredFeature(context.Background(), "crashed-job", true); err != nil {
		t.Fatal(err)
	}
	if err = reopened.RunMeteredFeature(context.Background(), "export", 1, "crashed-job", func() error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatal("settled job reran business", err, calls)
	}
	if err = reopened.RunMeteredFeature(context.Background(), "export", 2, "crashed-job", func() error { calls++; return nil }); err == nil || calls != 1 {
		t.Fatal("different quantity reused an old job")
	}
	if err = reopened.ResolveMeteredFeature(context.Background(), "crashed-job", false); err == nil {
		t.Fatal("completed outcome flipped")
	}
	if commits != 1 || reserves != 1 || cancels != 0 {
		t.Fatalf("unexpected remote settlement: %d %d %d", commits, reserves, cancels)
	}
	blockJournal, err = reopened.taskPath("storage-failure")
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.RunMeteredFeature(context.Background(), "export", 1, "storage-failure", func() error { calls++; return nil }); err == nil || calls != 1 || cancels != 1 {
		t.Fatal("business ran without its recovery journal", err, calls, cancels)
	}
	// Concurrent/reentrant actions are refused without touching the business.
	if err = reopened.RunMeteredFeature(context.Background(), "export", 1, "busy-parent", func() error {
		if inner := reopened.RunMeteredFeature(context.Background(), "export", 1, "busy-child", func() error { calls++; return nil }); inner == nil {
			t.Error("reentrant workflow was allowed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
