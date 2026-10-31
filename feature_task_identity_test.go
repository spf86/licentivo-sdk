package licentivo

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf86/licentivo-sdk/internal/lease"
)

func TestLaunchTaskIdentityAndLegacyJournal(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	const keyID = "11111111-1111-4111-8111-111111111111"
	const product = "33333333-3333-4333-8333-333333333333"
	const license = "44444444-4444-4444-8444-444444444444"
	const connection = "66666666-6666-4666-8666-666666666666"
	client := &Client{cfg: Config{BaseURL: "https://license.example.test", ProductID: product, DeviceID: strings.Repeat("a", 64), LaunchConnectionID: connection, Environment: "live", IdentityToken: "first-assertion", CachePath: filepath.Join(t.TempDir(), "lease.json")}, keys: map[string]ed25519.PublicKey{keyID: key.Public().(ed25519.PublicKey)}}
	setGrant := func(id string) {
		t.Helper()
		now := time.Now().Unix()
		envelope, err := lease.Sign(lease.Claims{Version: 1, KeyID: keyID, TenantID: "22222222-2222-4222-8222-222222222222", ProductID: product, LicenseID: id, DeviceID: client.cfg.DeviceID, ActivationID: "55555555-5555-4555-8555-555555555555", Mode: "timed", Features: []string{"export"}, MaxDevices: 1, IssuedAt: now, ExpiresAt: now + 1800, RuntimePolicy: &lease.RuntimePolicy{OfflineMode: "limited", HeartbeatSeconds: 600, OfflineSeconds: 1800}, Capabilities: &lease.Capabilities{LicenseModel: "node_locked", LaunchConnectionID: connection, Environment: "live", BindingVersion: 1, SessionID: "77777777-7777-4777-8777-777777777777", SessionExpiresAt: now + 3600}}, key)
		if err != nil {
			t.Fatal(err)
		}
		client.state.Lease = envelope
	}
	setGrant(license)
	legacy := client.cfg.CachePath + ".task-" + licenseHash(client.cacheLicenseHash()+"\njob") + ".json"
	if err := saveFeatureTask(legacy, featureTask{Version: 1, OperationID: "job", Feature: "export", Quantity: 1, ReservationID: "88888888-8888-4888-8888-888888888888", State: "business_running"}); err != nil {
		t.Fatal(err)
	}
	path, err := client.taskPath("job")
	if err != nil || path == legacy {
		t.Fatal("task namespace still depends on its short-lived assertion", path, err)
	}
	if task, err := readFeatureTask(path, "job"); err != nil || task.State != "business_running" {
		t.Fatal("legacy business outcome was lost", task, err)
	}
	client.cfg.IdentityToken, client.auth.DeviceKey = "renewed-assertion", "new-device-credential"
	if renewed, err := client.taskPath("job"); err != nil || renewed != path {
		t.Fatal("renewal changed task identity", renewed, err)
	}
	client.cfg.IdentityToken, client.auth.SourceHash = "", licenseHash("renewed-assertion")
	if reopened, err := client.taskPath("job"); err != nil || reopened != path {
		t.Fatal("retained-credential restart changed task identity", reopened, err)
	}
	setGrant("99999999-9999-4999-8999-999999999999")
	if other, err := client.taskPath("job"); err != nil || other == path {
		t.Fatal("different entitlements shared a task journal", other, err)
	}
	setGrant(license)
	client.state.Lease.Signature = strings.Repeat("A", len(client.state.Lease.Signature))
	if _, err := client.taskPath("job"); err == nil {
		t.Fatal("unverified claims selected a Launch task journal")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("a failed or unrelated login removed the original journal", err)
	}
}
