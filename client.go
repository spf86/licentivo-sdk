// Package licentivo provides the Go client for runtime licensing. No vendor management key is needed.
package licentivo

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf86/licentivo-sdk/internal/lease"
)

type Claims = lease.Claims

type Config struct {
	LaunchConnectionID string            `json:"launch_connection_id,omitempty"`
	IdentityToken      string            `json:"identity_token,omitempty"`
	Environment        string            `json:"environment,omitempty"`
	AppVersion         string            `json:"app_version"`
	BaseURL            string            `json:"base_url"`
	ProductID          string            `json:"product_id"`
	LicenseKey         string            `json:"license_key"`
	DeviceID           string            `json:"device_id"`
	DeviceName         string            `json:"device_name"`
	TrustedKeys        map[string]string `json:"trusted_keys"`
	CachePath          string            `json:"cache_path"`
	AllowHTTP          bool              `json:"allow_http"`
	TimeoutSeconds     int               `json:"timeout_seconds"`
	ProxyURL           string            `json:"proxy_url"`
}
type APIError struct {
	Status            int
	Code              string
	RequestID         string
	Retryable         bool
	RetryAfterSeconds int
	Cause             error
}

func (e *APIError) Error() string { return fmt.Sprintf("Licentivo %s (%d)", e.Code, e.Status) }
func (e *APIError) Unwrap() error { return e.Cause }

type Client struct {
	denied         atomic.Bool
	retryingLogout atomic.Bool
	logoutJournal  logoutJournal
	logoutBindings map[string]bool
	featureTaskMu  sync.Mutex
	auth           credentialState
	authDirty      bool
	view           atomic.Pointer[localView]
	listener       atomic.Pointer[statusListener]
	waitHeartbeat  func(context.Context, time.Duration) bool
	profile        DeviceProfile
	sessionID      string
	cfg            Config
	keys           map[string]ed25519.PublicKey
	http           *http.Client
	mu             sync.Mutex
	state          cache
	heartbeat      int
	lastWall       time.Time
	lastTick       time.Time
	cancel         context.CancelFunc
	done           chan struct{}
	workersMu      sync.Mutex
	refreshCancel  context.CancelFunc
	refreshDone    chan struct{}
	closed         atomic.Bool
}
type cache struct {
	Version     int            `json:"version"`
	LicenseHash string         `json:"license_hash"`
	Lease       lease.Envelope `json:"lease"`
	ObservedAt  int64          `json:"observed_at"`
}
type response struct {
	LogoutTicket *LogoutTicket   `json:"logout_ticket,omitempty"`
	DeviceKey    string          `json:"device_key,omitempty"`
	Consumption  *Consumption    `json:"consumption,omitempty"`
	Reservation  *Reservation    `json:"reservation,omitempty"`
	ActivationID string          `json:"activation_id"`
	Lease        *lease.Envelope `json:"lease"`
	Heartbeat    int             `json:"heartbeat_seconds"`
	Deactivated  bool            `json:"deactivated"`
}

func DeviceID(product, stableInstallationID string) string {
	sum := sha256.Sum256([]byte("licentivo-device-v1\n" + product + "\n" + strings.ToLower(strings.TrimSpace(stableInstallationID))))
	return hex.EncodeToString(sum[:])
}
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://www.licentivo.com"
	}
	profile, device, err := MachineProfile(cfg.ProductID)
	if err != nil {
		return nil, err
	}
	cfg.DeviceID = device
	u, e := url.Parse(cfg.BaseURL)
	if e != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("base_url must be an origin")
	}
	if u.Scheme != "https" && !(cfg.AllowHTTP && u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback())) {
		return nil, errors.New("HTTPS required; allow_http is restricted to local tests")
	}
	if !lease.ValidDevice(cfg.DeviceID) || cfg.LicenseKey != "" && (len(cfg.LicenseKey) != 50 || !(strings.HasPrefix(cfg.LicenseKey, "lv_lic_") || temporaryKey(cfg.LicenseKey))) {
		return nil, errors.New("invalid device or license key")
	}
	keys := map[string]ed25519.PublicKey{}
	for id, value := range cfg.TrustedKeys {
		b, e := base64.RawURLEncoding.Strict().DecodeString(value)
		if e != nil || len(b) != 32 {
			return nil, errors.New("invalid pinned public key")
		}
		keys[id] = b
	}
	if len(keys) == 0 {
		return nil, errors.New("pin at least one product public key")
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.CachePath == "" {
		root, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		cfg.CachePath = filepath.Join(root, "Licentivo", licenseHash(cfg.BaseURL+"\n"+cfg.ProductID), "lease.json")
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 10
	}
	if cfg.TimeoutSeconds < 1 || cfg.TimeoutSeconds > 120 {
		return nil, errors.New("timeout_seconds must be 1–120")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.ProxyURL != "" {
		proxy, err := url.Parse(cfg.ProxyURL)
		if err != nil || proxy.Host == "" || (proxy.Scheme != "http" && proxy.Scheme != "https") {
			return nil, errors.New("proxy_url must be an HTTP(S) URL")
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	c := &Client{cfg: cfg, profile: profile, sessionID: requestUUID(), keys: keys, http: &http.Client{Transport: transport, Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, heartbeat: 3600}
	c.loadCredential()
	c.loadLogoutJournal()
	if c.denied.Load() {
		c.auth = credentialState{}
	}
	if cfg.LicenseKey == "" && cfg.IdentityToken == "" && c.auth.DeviceKey == "" && !c.denied.Load() {
		return nil, errors.New("enter a license key or temporary activation code")
	}
	if cfg.CachePath != "" && !c.denied.Load() {
		if info, e := os.Stat(cfg.CachePath); e == nil && info.Size() <= lease.MaxSize {
			if b, e := os.ReadFile(cfg.CachePath); e == nil {
				var s cache
				if lease.Decode(b, &s) == nil && s.Version == 1 && s.ObservedAt > 0 && s.LicenseHash == c.cacheLicenseHash() {
					if claims, ve := lease.Verify(s.Lease, keys, cfg.ProductID, cfg.DeviceID, time.Now()); ve == nil && claims.OfflineMode() != "none" && time.Now().Unix()+300 >= s.ObservedAt && (claims.Capabilities == nil || claims.Capabilities.LicenseModel != "floating") && versionContext(claims, cfg.AppVersion) == nil {
						c.state = s
						if claims.RuntimePolicy != nil {
							c.heartbeat = claims.RuntimePolicy.HeartbeatSeconds
						}
						c.lastWall = time.Unix(s.ObservedAt, 0)
						c.lastTick = time.Now()
					}
				}
			}
		}
	}
	c.publish(false, nil)
	if len(c.logoutJournal.Pending) > 0 {
		c.startLogoutRecovery()
	}
	return c, nil
}
func licenseHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (c *Client) Activate(ctx context.Context) (lease.Claims, error) {
	return c.call(ctx, "activate", false)
}
func (c *Client) Validate(ctx context.Context) (lease.Claims, error) {
	return c.call(ctx, "validate", false)
}
func (c *Client) Heartbeat(ctx context.Context) (lease.Claims, error) {
	return c.call(ctx, "heartbeat", false)
}

// RefreshOnline always contacts the server, even with indefinite offline authorization.
// Refresh the current policy online. Network failure keeps a valid cache; explicit denial clears it.
func (c *Client) RefreshOnline(ctx context.Context) (lease.Claims, error) {
	return c.call(ctx, "activate", true)
}
func (c *Client) check() (lease.Claims, error) {
	if c.denied.Load() {
		return lease.Claims{}, &APIError{Code: "LAUNCH_LOGIN_REQUIRED"}
	}
	now := time.Now()
	if !c.lastWall.IsZero() {
		elapsed := time.Since(c.lastTick)
		expected := c.lastWall.Add(elapsed)
		if now.Before(expected.Add(-5 * time.Minute)) {
			return lease.Claims{}, errors.New("clock moved backwards; online verification required")
		}
	}
	claims, err := lease.Verify(c.state.Lease, c.keys, c.cfg.ProductID, c.cfg.DeviceID, now)
	if err == nil {
		err = versionContext(claims, c.cfg.AppVersion, c.sessionID)
	}
	if err == nil {
		err = c.accountContext(claims)
	}
	return claims, err
}
func (c *Client) CheckFeature(feature string) bool {
	return c.FeatureStatus(feature).Allowed
}
func (c *Client) call(ctx context.Context, operation string, forceOnline bool) (lease.Claims, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.denied.Load() {
		return lease.Claims{}, &APIError{Code: "LAUNCH_LOGIN_REQUIRED"}
	}
	if claims, err := c.check(); !forceOnline && operation == "activate" && err == nil && claims.OfflineMode() == "permanent" && claims.RuntimePolicy.HeartbeatSeconds == 0 {
		return claims, nil
	}
	activation := ""
	if operation != "activate" {
		if claims, e := c.check(); e == nil {
			activation = claims.ActivationID
		} else if c.state.Lease.Payload != "" {
			b, _ := base64.RawURLEncoding.DecodeString(c.state.Lease.Payload)
			var claims lease.Claims
			if json.Unmarshal(b, &claims) == nil {
				activation = claims.ActivationID
			}
		}
		if activation == "" {
			return lease.Claims{}, errors.New("activate before validation")
		}
	}
	out, temporary, e := c.request(ctx, operation, activation)
	if c.denied.Load() {
		if e == nil && out.LogoutTicket != nil {
			c.auth.LogoutTicket = out.LogoutTicket
		}
		return lease.Claims{}, &APIError{Code: "LAUNCH_LOGIN_REQUIRED"}
	}
	if e != nil {
		if explicitRevocation(e) {
			c.denyLocked()
			c.publish(false, e)
			return lease.Claims{}, e
		}
		if !forceOnline && temporary && ctx.Err() == nil {
			if claims, ve := c.check(); ve == nil && claims.OfflineMode() != "none" {
				c.publish(false, e)
				return claims, nil
			}
		}
		if claims, ve := c.check(); !temporary || ve == nil && claims.OfflineMode() == "none" {
			c.invalidate()
		}
		c.publish(false, e)
		return lease.Claims{}, e
	}
	if out.Lease == nil {
		return lease.Claims{}, errors.New("server omitted signed lease")
	}
	claims, e := lease.Verify(*out.Lease, c.keys, c.cfg.ProductID, c.cfg.DeviceID, time.Now())
	if e == nil {
		e = versionContext(claims, c.cfg.AppVersion, c.sessionID)
	}
	if e == nil {
		e = c.accountContext(claims)
	}
	if e != nil {
		c.invalidate()
		return lease.Claims{}, e
	}
	if claims.ActivationID != out.ActivationID {
		c.invalidate()
		return lease.Claims{}, errors.New("activation context mismatch")
	}
	if e = c.acceptCredential(out); e != nil {
		return lease.Claims{}, e
	}
	c.state = cache{1, c.cacheLicenseHash(), *out.Lease, time.Now().Unix()}
	c.lastWall = time.Now()
	c.lastTick = time.Now()
	if out.Heartbeat >= 10 && out.Heartbeat <= 86400 {
		c.heartbeat = out.Heartbeat
	}
	if claims.RuntimePolicy != nil {
		c.heartbeat = claims.RuntimePolicy.HeartbeatSeconds
	}
	if e = c.save(); e != nil {
		return lease.Claims{}, e
	}
	c.publish(true, nil)
	return claims, nil
}
func (c *Client) request(ctx context.Context, op, activation string) (response, bool, error) {
	return c.requestExtra(ctx, op, activation, "", nil)
}
func (c *Client) requestExtra(ctx context.Context, op, activation, suppliedID string, extra map[string]any) (response, bool, error) {
	if err := c.prepareCredential(); err != nil {
		return response{}, false, err
	}
	in := c.runtimeBody(activation)
	for k, v := range extra {
		in[k] = v
	}
	body, _ := json.Marshal(in)
	id := make([]byte, 16)
	if _, e := rand.Read(id); e != nil {
		return response{}, false, e
	}
	idem := hex.EncodeToString(id)
	if suppliedID != "" {
		idem = suppliedID
	}
	if op == "activate" && c.auth.DeviceKey == "" && c.state.Lease.Payload == "" {
		idem = c.auth.RequestID
	}
	var final error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return response{}, true, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
		path := op
		if op == "activate" && c.auth.DeviceKey == "" && c.cfg.IdentityToken != "" {
			path = "launch/exchange"
		}
		req, e := http.NewRequestWithContext(ctx, "POST", c.cfg.BaseURL+"/api/v1/"+path, bytes.NewReader(body))
		if e != nil {
			return response{}, false, e
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idem)
		res, e := c.http.Do(req)
		if e != nil {
			final = &APIError{Code: "NETWORK_UNAVAILABLE", Retryable: true, Cause: e}
			continue
		}
		b, e := io.ReadAll(io.LimitReader(res.Body, lease.MaxSize+1))
		res.Body.Close()
		if e != nil {
			final = &APIError{Code: "NETWORK_UNAVAILABLE", Retryable: true, Cause: e}
			continue
		}
		if len(b) > lease.MaxSize {
			return response{}, false, errors.New("oversized server response")
		}
		var env struct {
			Data  response `json:"data"`
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			RequestID string `json:"request_id"`
		}
		if e = lease.Decode(b, &env); e != nil {
			if res.StatusCode == 429 || res.StatusCode >= 500 {
				retry, _ := strconv.Atoi(res.Header.Get("Retry-After"))
				apiErr := &APIError{Status: res.StatusCode, Code: "UNEXPECTED_RESPONSE", RequestID: res.Header.Get("X-Request-ID"), Retryable: true, RetryAfterSeconds: min(86400, max(0, retry)), Cause: e}
				if res.StatusCode == 429 {
					return response{}, true, apiErr
				}
				final = apiErr
				continue
			}
			return response{}, false, e
		}
		if res.StatusCode != 200 || env.Error != nil {
			code := "UNEXPECTED_RESPONSE"
			if env.Error != nil {
				code = env.Error.Code
			}
			retryAfter, _ := strconv.Atoi(res.Header.Get("Retry-After"))
			if retryAfter < 0 {
				retryAfter = 0
			}
			if retryAfter > 86400 {
				retryAfter = 86400
			}
			apiErr := &APIError{Status: res.StatusCode, Code: code, RequestID: env.RequestID, Retryable: res.StatusCode == 429 || res.StatusCode >= 500, RetryAfterSeconds: retryAfter}
			if res.StatusCode >= 500 {
				final = apiErr
				continue
			}
			return response{}, apiErr.Retryable, apiErr
		}
		return env.Data, false, nil
	}
	return response{}, true, final
}
func (c *Client) save() error {
	if c.cfg.CachePath == "" {
		return nil
	}
	if e := os.MkdirAll(filepath.Dir(c.cfg.CachePath), 0700); e != nil {
		return e
	}
	b, _ := json.Marshal(c.state)
	tmp, e := os.CreateTemp(filepath.Dir(c.cfg.CachePath), ".lease-*")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	if e = tmp.Chmod(0600); e == nil {
		_, e = tmp.Write(b)
	}
	if ce := tmp.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(tmp.Name(), c.cfg.CachePath)
}
func (c *Client) invalidate() {
	c.state = cache{}
	if c.cfg.CachePath != "" && !c.denied.Load() {
		_ = os.Remove(c.cfg.CachePath)
	}
	c.publish(false, nil)
}
func (c *Client) Deactivate(ctx context.Context) error {
	_, err := c.Logout(ctx)
	return err
}

// StartHeartbeat owns one cancellable goroutine. Errors are delivered to the callback, without credentials.
func (c *Client) StartHeartbeat(ctx context.Context, onError func(error)) error {
	c.mu.Lock()
	if c.heartbeat == 0 {
		c.mu.Unlock()
		return nil
	}
	if c.cancel != nil {
		c.mu.Unlock()
		return nil
	}
	ctx, c.cancel = context.WithCancel(ctx)
	done := make(chan struct{})
	c.done = done
	c.mu.Unlock()
	go func() {
		defer func() {
			c.mu.Lock()
			if c.done == done {
				c.cancel = nil
			}
			c.mu.Unlock()
			close(done)
		}()
		for {
			c.mu.Lock()
			interval := c.heartbeat
			c.mu.Unlock()
			if interval == 0 {
				return
			}
			var jitter [1]byte
			_, _ = rand.Read(jitter[:])
			delay := heartbeatDelay(interval, jitter[0])
			if view := c.view.Load(); view != nil && view.retryAt.After(time.Now().Add(delay)) {
				delay = time.Until(view.retryAt)
			}
			wait := c.waitHeartbeat
			if wait == nil {
				wait = waitHeartbeat
			}
			if !wait(ctx, delay) {
				return
			}
			if _, e := c.Heartbeat(ctx); e != nil && ctx.Err() == nil && onError != nil {
				go onError(e)
			}
		}
	}()
	return nil
}
func (c *Client) stopHeartbeat() {
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.cancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// Close stops heartbeats and returns a floating seat, with a bounded network wait.
func (c *Client) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Stop(ctx)
	c.http.CloseIdleConnections()
}
