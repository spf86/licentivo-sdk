package licentivo

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf86/licentivo-sdk/internal/lease"
)

func TestLogoutDuringIdentityExchangeNeverRestoresAuthority(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	product := "33333333-3333-4333-8333-333333333333"
	connection := "66666666-6666-4666-8666-666666666666"
	keyID := "11111111-1111-4111-8111-111111111111"
	activation := "55555555-5555-4555-8555-555555555555"
	device, err := MachineDeviceID(product)
	if err != nil {
		t.Fatal(err)
	}
	started, finish := make(chan struct{}), make(chan struct{})
	var revocations atomic.Int32
	ticket := LogoutTicket{OperationID: "77777777-7777-4777-8777-777777777777", ProductID: product, ActivationID: activation, ExpectedGeneration: 1, ReleaseToken: "lv_out_" + strings.Repeat("o", 43)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sessions/logout" {
			revocations.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "succeeded", "operation_id": ticket.OperationID, "activation_id": activation, "expected_generation": 1}})
			return
		}
		close(started)
		<-finish
		now := time.Now().Unix()
		signed, _ := lease.Sign(lease.Claims{Version: 1, KeyID: keyID, TenantID: "22222222-2222-4222-8222-222222222222", ProductID: product, LicenseID: "44444444-4444-4444-8444-444444444444", DeviceID: device, ActivationID: activation, Mode: "perpetual", Features: []string{"export"}, MaxDevices: 1, IssuedAt: now, ExpiresAt: now + 1200, RuntimePolicy: &lease.RuntimePolicy{OfflineMode: "limited", HeartbeatSeconds: 600, OfflineSeconds: 1200}, Capabilities: &lease.Capabilities{LicenseModel: "node_locked", SessionID: "88888888-8888-4888-8888-888888888888", SessionExpiresAt: now + 3600, LaunchConnectionID: connection, Environment: "live", BindingVersion: 1}}, key)
		json.NewEncoder(w).Encode(map[string]any{"data": response{ActivationID: activation, DeviceKey: "lv_dev_" + strings.Repeat("d", 43), Lease: &signed, Heartbeat: 600, LogoutTicket: &ticket}, "request_id": "exchange"})
	}))
	defer server.Close()
	cache := filepath.Join(t.TempDir(), "lease.json")
	client, err := New(Config{BaseURL: server.URL, ProductID: product, TrustedKeys: map[string]string{keyID: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}, LaunchConnectionID: connection, Environment: "live", IdentityToken: "in-memory-assertion", CachePath: cache, AllowHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	activated := make(chan error, 1)
	go func() { _, err := client.Activate(context.Background()); activated <- err }()
	<-started
	loggedOut := make(chan LogoutResult, 1)
	go func() {
		result, err := client.Logout(context.Background())
		if err != nil {
			t.Error(err)
		}
		loggedOut <- result
	}()
	deadline := time.Now().Add(time.Second)
	for !client.denied.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !client.denied.Load() || client.CheckFeature("export") || client.Status().Allowed {
		t.Fatal("logout waited for the in-flight network before denying protected work")
	}
	close(finish)
	if err := <-activated; err == nil {
		t.Fatal("an exchange completed authorization after logout")
	}
	result := <-loggedOut
	if !result.LocalComplete || result.RevocationPending || revocations.Load() != 1 {
		t.Fatal("in-flight logout ticket was lost", result, revocations.Load())
	}
	resumed, err := New(client.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if resumed.CheckFeature("export") || !resumed.denied.Load() {
		t.Fatal("logout did not survive restart")
	}
	if _, err := client.Activate(context.Background()); err == nil {
		t.Fatal("logged-out client automatically reactivated")
	}
}

type failedLogoutTransport struct{ calls atomic.Int32 }

func (t *failedLogoutTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return nil, errors.New("offline")
}

func cachedLogoutClient(t *testing.T, path string, generation int) *Client {
	t.Helper()
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	keyID, product, activation := "11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333", "55555555-5555-4555-8555-555555555555"
	c, err := New(Config{BaseURL: "https://example.test", ProductID: product, IdentityToken: "login-assertion", CachePath: path, TrustedKeys: map[string]string{keyID: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}})
	if err != nil {
		t.Fatal(err)
	}
	c.auth = credentialState{Version: 1, SourceHash: licenseHash("login-assertion"), ProductID: product, Origin: c.cfg.BaseURL, DeviceID: c.cfg.DeviceID, DeviceKey: "lv_dev_" + strings.Repeat("d", 43), RequestID: requestUUID(), SessionID: requestUUID(), LogoutTicket: &LogoutTicket{OperationID: requestUUID(), ProductID: product, ActivationID: activation, ExpectedGeneration: generation, ReleaseToken: "lv_out_" + strings.Repeat("o", 43)}}
	if err = c.saveCredential(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	signed, err := lease.Sign(lease.Claims{Version: 1, KeyID: keyID, TenantID: "22222222-2222-4222-8222-222222222222", ProductID: product, LicenseID: "44444444-4444-4444-8444-444444444444", DeviceID: c.cfg.DeviceID, ActivationID: activation, Mode: "perpetual", Features: []string{"export"}, MaxDevices: 1, IssuedAt: now, ExpiresAt: now + 1200, RuntimePolicy: &lease.RuntimePolicy{OfflineMode: "limited", HeartbeatSeconds: 600, OfflineSeconds: 1200}}, key)
	if err != nil {
		t.Fatal(err)
	}
	c.state = cache{Version: 1, LicenseHash: c.cacheLicenseHash(), Lease: signed, ObservedAt: now}
	if err = c.save(); err != nil {
		t.Fatal(err)
	}
	c.publish(false, nil)
	t.Cleanup(func() { c.Close() })
	return c
}
func isolatedLogoutCache(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("XDG_CACHE_HOME", root)
	return filepath.Join(t.TempDir(), "lease.json")
}
func TestLogoutFallbackSurvivesPrimaryFailureAndOfflineRestart(t *testing.T) {
	path := isolatedLogoutCache(t)
	c := cachedLogoutClient(t, path, 1)
	transport := &failedLogoutTransport{}
	c.http = &http.Client{Transport: transport}
	if err := os.Mkdir(path+".logout.json", 0700); err != nil {
		t.Fatal(err)
	}
	result, err := c.Logout(context.Background())
	if err != nil || !result.LocalComplete || !result.RevocationPending || transport.calls.Load() != 1 || c.CheckFeature("export") {
		t.Fatal(result, err, transport.calls.Load())
	}
	// A failed atomic replacement may leave a perfectly readable old journal.
	if err = os.Remove(path + ".logout.json"); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(logoutJournal{Version: 1, SourceHash: licenseHash("login-assertion")})
	if err = os.WriteFile(path+".logout.json", b, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := c.cfg
	cfg.IdentityToken = ""
	restart, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restart.Close()
	if restart.CheckFeature("export") || restart.Status().Allowed || !restart.denied.Load() {
		t.Fatal("old cache restored authority after failed logout persistence")
	}
	if _, err = restart.Activate(context.Background()); err == nil {
		t.Fatal("signed-out credential reactivated")
	}
}
func TestLogoutReportsTotalPersistenceFailureAndStillRevokes(t *testing.T) {
	path := isolatedLogoutCache(t)
	c := cachedLogoutClient(t, path, 1)
	transport := &failedLogoutTransport{}
	c.http = &http.Client{Transport: transport}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", blocked)
	t.Setenv("XDG_CACHE_HOME", blocked)
	if err := os.Mkdir(path+".logout.json", 0700); err != nil {
		t.Fatal(err)
	}
	result, err := c.Logout(context.Background())
	if err == nil || result.LocalComplete || !result.RevocationPending || transport.calls.Load() != 1 || c.CheckFeature("export") {
		t.Fatal(result, err, transport.calls.Load())
	}
}
func TestOldInstanceLogoutPreservesNewBindingCache(t *testing.T) {
	path := isolatedLogoutCache(t)
	old := cachedLogoutClient(t, path, 1)
	old.http = &http.Client{Transport: &failedLogoutTransport{}}
	newer := cachedLogoutClient(t, path, 2)
	before := map[string]string{}
	for _, p := range []string{path, path + ".credential.json"} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		before[p] = string(b)
	}
	if result, err := old.Logout(context.Background()); err != nil || !result.LocalComplete {
		t.Fatal(result, err)
	}
	for p, b := range before {
		after, err := os.ReadFile(p)
		if err != nil || string(after) != b {
			t.Fatal("old logout changed newer binding", p, err)
		}
	}
	cfg := newer.cfg
	cfg.IdentityToken = ""
	restart, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restart.Close()
	if restart.denied.Load() || !restart.CheckFeature("export") || restart.auth.LogoutTicket.ExpectedGeneration != 2 {
		t.Fatal("new binding could not recover after old instance logout")
	}
}

type acknowledgedLogoutTransport struct{}

func (*acknowledgedLogoutTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var ticket LogoutTicket
	if err := json.NewDecoder(r.Body).Decode(&ticket); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(map[string]any{"data": map[string]any{"status": "stale", "operation_id": ticket.OperationID, "activation_id": ticket.ActivationID, "expected_generation": ticket.ExpectedGeneration}})
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
}
func TestNewBindingRecoveryAcknowledgesOldLogoutRecord(t *testing.T) {
	path := isolatedLogoutCache(t)
	old := cachedLogoutClient(t, path, 1)
	old.http = &http.Client{Transport: &failedLogoutTransport{}}
	newer := cachedLogoutClient(t, path, 2)
	if _, err := old.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	old.Close()
	cfg := newer.cfg
	cfg.IdentityToken = ""
	restart, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	restart.http = &http.Client{Transport: &acknowledgedLogoutTransport{}}
	if err = restart.RetryPendingLogouts(context.Background()); err != nil {
		t.Fatal(err)
	}
	restart.Close()
	final, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer final.Close()
	if len(final.logoutJournal.Pending) != 0 || !final.CheckFeature("export") {
		t.Fatal("confirmed old logout was replayed or blocked the new binding")
	}
}

func TestLogoutNewestRecordWinsAcrossStorageLocations(t *testing.T) {
	for _, signedOut := range []bool{true, false} {
		t.Run(fmt.Sprint(signedOut), func(t *testing.T) {
			path := isolatedLogoutCache(t)
			c := cachedLogoutClient(t, path, 1)
			dir, err := c.logoutDirectory()
			if err != nil {
				t.Fatal(err)
			}
			j := logoutJournal{Version: 1, Revision: 10, SourceHash: c.auth.SourceHash, BindingKey: logoutBinding(c.auth.LogoutTicket), SignedOut: !signedOut, Pending: []LogoutTicket{}}
			fallback := filepath.Join(dir, licenseHash(j.SourceHash+"\n"+j.BindingKey)+".json")
			if err = writeLogoutJournal(fallback, j); err != nil {
				t.Fatal(err)
			}
			j.Revision = 11
			j.SignedOut = signedOut
			if err = writeLogoutJournal(path+".logout.json", j); err != nil {
				t.Fatal(err)
			}
			cfg := c.cfg
			cfg.IdentityToken = ""
			restart, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer restart.Close()
			if restart.denied.Load() != signedOut || restart.CheckFeature("export") == signedOut {
				t.Fatal("a stale secondary record replaced the newer persisted decision")
			}
		})
	}
}

func TestRotatedAssertionCannotRestoreLoggedOutBinding(t *testing.T) {
	path := isolatedLogoutCache(t)
	old := cachedLogoutClient(t, path, 1)
	ticket := old.auth.LogoutTicket
	old.http = &http.Client{Transport: &failedLogoutTransport{}}
	if _, err := old.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	old.Close()
	cfg := old.cfg
	cfg.IdentityToken = "new-assertion-for-the-old-launch-login"
	cfg.LaunchConnectionID = "66666666-6666-4666-8666-666666666666"
	restart, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restart.Close()
	restart.http = &http.Client{Transport: &failedLogoutTransport{}}
	err = restart.acceptCredential(response{LogoutTicket: ticket, DeviceKey: "lv_dev_" + strings.Repeat("d", 43)})
	var api *APIError
	if !errors.As(err, &api) || api.Code != "LAUNCH_LOGIN_REQUIRED" || !restart.denied.Load() || restart.CheckFeature("export") {
		t.Fatal("a rotated assertion restored a logged-out binding", err)
	}
}
