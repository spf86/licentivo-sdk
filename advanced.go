package licentivo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/spf86/licentivo-sdk/internal/lease"
	"os"
	"strings"
	"time"
)

type Consumption struct {
	Feature   string     `json:"feature"`
	Period    string     `json:"period"`
	Quantity  int64      `json:"quantity"`
	Used      int64      `json:"used"`
	Limit     int64      `json:"limit"`
	Remaining int64      `json:"remaining"`
	Overage   int64      `json:"overage"`
	Reserved  int64      `json:"reserved"`
	ResetAt   *time.Time `json:"reset_at"`
}

func requestUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure randomness unavailable")
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[:8], h[8:12], h[12:16], h[16:20], h[20:])
}
func versionContext(c lease.Claims, version string, sessions ...string) error {
	if c.Capabilities != nil && c.Capabilities.AppVersion != version {
		return errors.New("cached lease belongs to another application version; verify online")
	}
	if c.Capabilities != nil && c.Capabilities.LicenseModel == "floating" && (len(sessions) == 0 || c.Capabilities.SessionID != sessions[0]) {
		return errors.New("floating lease belongs to another running session")
	}
	return nil
}
func (c *Client) runtimeBody(activation string) map[string]any {
	body := map[string]any{"product_id": c.cfg.ProductID, "license_key": c.runtimeKey(), "device_id": c.cfg.DeviceID, "device_name": c.cfg.DeviceName, "activation_id": activation, "app_version": c.cfg.AppVersion, "session_id": c.sessionID, "device_profile": c.profile}
	details := c.deviceDetails
	body["device_info"] = map[string]any{"os_version": details.OSVersion, "cpu": details.CPU, "memory_mb": details.MemoryMB, "gpu": details.GPU, "system_type": details.SystemType, "storage_gb": details.StorageGB}
	if c.auth.LogoutTicket == nil {
		body["request_logout_ticket"] = true
	}
	if c.cfg.IdentityToken != "" && c.auth.DeviceKey == "" {
		body["identity_token"] = c.cfg.IdentityToken
		body["launch_connection_id"] = c.cfg.LaunchConnectionID
		body["environment"] = c.cfg.Environment
	}
	if c.cfg.LaunchConnectionID != "" {
		body["launch_connection_id"] = c.cfg.LaunchConnectionID
		body["environment"] = c.cfg.Environment
	}
	return body
}

func (c *Client) accountContext(claims lease.Claims) error {
	connection, environment := "", ""
	if x := claims.Capabilities; x != nil {
		connection = x.LaunchConnectionID
		environment = x.Environment
	}
	if connection != c.cfg.LaunchConnectionID || environment != c.cfg.Environment {
		return errors.New("lease belongs to another Launch connection or environment")
	}
	return nil
}

// Start performs activation and starts the policy-controlled heartbeat loop.
func (c *Client) Start(ctx context.Context, onError func(error)) (lease.Claims, error) {
	if c.closed.Load() {
		return lease.Claims{}, errors.New("client is closed")
	}
	c.mu.Lock()
	claims, cachedErr := c.check()
	c.mu.Unlock()
	if cachedErr == nil && claims.OfflineMode() != "none" && (claims.Capabilities == nil || claims.Capabilities.LicenseModel != "floating") {
		if err := c.StartHeartbeat(ctx, onError); err != nil {
			return claims, err
		}
		c.startRefresh(ctx, onError)
		return claims, nil
	}
	claims, err := c.Activate(ctx)
	if err != nil {
		return claims, err
	}
	return claims, c.StartHeartbeat(ctx, onError)
}

// OpenProduct starts a client using the product ID and public keys copied from
// Console > Products > Signing public keys. Keep it alive until app shutdown.
func OpenProduct(ctx context.Context, productID string, trustedKeys map[string]string, licenseKey string, onError func(error)) (*Client, error) {
	c, err := New(Config{ProductID: productID, TrustedKeys: trustedKeys, LicenseKey: strings.TrimSpace(licenseKey)})
	if err != nil {
		return nil, err
	}
	if _, err = c.Start(ctx, onError); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func Open(ctx context.Context, configPath string, onError func(error)) (*Client, error) {
	return openWithConfig(ctx, configPath, nil, onError)
}

// OpenWithLicense uses shared product settings and the customer's own license key.
// Read the customer's license key from the application UI or private user settings.
func OpenWithLicense(ctx context.Context, configPath, licenseKey string, onError func(error)) (*Client, error) {
	key := strings.TrimSpace(licenseKey)
	return openWithConfig(ctx, configPath, &key, onError)
}

func openWithConfig(ctx context.Context, configPath string, licenseKey *string, onError func(error)) (*Client, error) {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err = json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	if licenseKey != nil {
		cfg.LicenseKey = *licenseKey
	}
	c, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if _, err = c.Start(ctx, onError); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Stop releases floating seats. A node-locked device remains registered until Deactivate is called.
func (c *Client) Stop(ctx context.Context) error {
	c.stopRefresh()
	c.stopHeartbeat()
	c.mu.Lock()
	claims, err := c.check()
	c.mu.Unlock()
	if err == nil && claims.Capabilities != nil && claims.Capabilities.LicenseModel == "floating" {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.invalidate()
		_, _, err = c.request(ctx, "deactivate", claims.ActivationID)
		return err
	}
	return nil
}

// Retain operationID with the business job and reuse it if the request times out.
func (c *Client) Consume(ctx context.Context, feature string, quantity int64, operationID string) (Consumption, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	claims, err := c.check()
	if err != nil {
		return Consumption{}, err
	}
	if operationID == "" {
		return Consumption{}, errors.New("operationID required for safe consumption retries")
	}
	out, _, err := c.requestExtra(ctx, "consume", claims.ActivationID, operationID, map[string]any{"feature": feature, "quantity": quantity})
	if err != nil {
		if deniedAuthorization(err) {
			c.denyLocked()
		}
		c.publish(false, err)
		return Consumption{}, err
	}
	if out.Consumption == nil {
		return Consumption{}, errors.New("server omitted consumption receipt")
	}
	c.publish(true, nil)
	return *out.Consumption, nil
}
func (c *Client) OfflineRequest(operation string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if operation != "activate" && operation != "deactivate" {
		return nil, errors.New("operation must be activate or deactivate")
	}
	if c.denied.Load() {
		return nil, errors.New("enter a new activation input before exporting an offline request")
	}
	if err := c.prepareCredential(); err != nil {
		return nil, err
	}
	activation := ""
	if operation == "deactivate" {
		claims, err := c.check()
		if err != nil {
			return nil, err
		}
		activation = claims.ActivationID
	}
	in := c.runtimeBody(activation)
	in["version"] = 1
	requestID := requestUUID()
	in["request_id"] = requestID
	in["created_at"] = time.Now().Unix()
	in["operation"] = operation
	if operation == "activate" {
		c.auth.OfflineRequestID, c.authDirty = requestID, true
		if err := c.saveCredential(); err != nil {
			return nil, err
		}
		if c.logoutJournal.SignedOut {
			c.logoutJournal.OfflineRequestID = requestID
			if err := c.saveLogoutJournal(); err != nil {
				return nil, err
			}
		}
	}
	request, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return nil, err
	}
	if operation == "deactivate" {
		if err := c.denyBindingLocked(false); err != nil {
			return nil, err
		}
		if c.cancel != nil {
			c.cancel()
		}
	}
	return request, nil
}

// ImportOffline verifies the signature and original request ID before accepting the response.
func (c *Client) ImportOffline(request, responseJSON []byte) (lease.Claims, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.denied.Load() {
		return lease.Claims{}, &APIError{Code: "LAUNCH_LOGIN_REQUIRED"}
	}
	var req struct {
		RequestID     string `json:"request_id"`
		ProductID     string `json:"product_id"`
		DeviceID      string `json:"device_id"`
		LicenseKey    string `json:"license_key"`
		IdentityToken string `json:"identity_token"`
		Operation     string `json:"operation"`
	}
	if err := json.Unmarshal(request, &req); err != nil {
		return lease.Claims{}, err
	}
	var res struct {
		Version   int      `json:"version"`
		RequestID string   `json:"request_id"`
		Operation string   `json:"operation"`
		Result    response `json:"result"`
	}
	if err := lease.Decode(responseJSON, &res); err != nil {
		return lease.Claims{}, err
	}
	// A previous attempt may have saved the device credential before the lease.
	// Its recorded source still identifies the original offline activation input.
	requestSource := req.LicenseKey
	if req.IdentityToken != "" {
		requestSource = req.IdentityToken
	}
	sameInput := req.LicenseKey == c.runtimeKey() && (req.IdentityToken == "" || req.IdentityToken == c.cfg.IdentityToken)
	sameSource := requestSource != "" && (sameInput || c.auth.DeviceKey != "" && licenseHash(requestSource) == c.auth.SourceHash)
	if res.Version != 1 || res.Operation != "activate" || req.Operation != "activate" || req.ProductID != c.cfg.ProductID || req.DeviceID != c.cfg.DeviceID || !sameSource || req.RequestID != res.RequestID || res.Result.Lease == nil {
		return lease.Claims{}, errors.New("offline response context mismatch")
	}
	currentRequest := c.auth.OfflineRequestID
	if currentRequest == "" || c.logoutJournal.SignedOut {
		currentRequest = c.logoutJournal.OfflineRequestID
	}
	if currentRequest != "" && currentRequest != req.RequestID || c.logoutJournal.SignedOut && currentRequest == "" {
		return lease.Claims{}, errors.New("offline approval is no longer current; generate a fresh request")
	}
	claims, err := lease.Verify(*res.Result.Lease, c.keys, c.cfg.ProductID, c.cfg.DeviceID, time.Now())
	if err != nil {
		return claims, err
	}
	if claims.OfflineMode() == "none" || claims.OfflineRequestID != req.RequestID || claims.ActivationID != res.Result.ActivationID {
		return claims, errors.New("offline signature context mismatch")
	}
	if err = versionContext(claims, c.cfg.AppVersion); err != nil {
		return claims, err
	}
	if err = c.accountContext(claims); err != nil {
		return claims, err
	}
	if err = c.acceptCredential(res.Result); err != nil {
		return claims, err
	}
	if err = c.prepareCredential(); err != nil {
		return claims, err
	}
	if c.auth.OfflineRequestID != req.RequestID {
		c.auth.OfflineRequestID, c.authDirty = req.RequestID, true
		if err = c.saveCredential(); err != nil {
			return claims, err
		}
	}
	c.state = cache{1, c.cacheLicenseHash(), *res.Result.Lease, time.Now().Unix()}
	c.lastWall = time.Now()
	c.lastTick = time.Now()
	c.heartbeat = 3600
	if claims.RuntimePolicy != nil {
		c.heartbeat = claims.RuntimePolicy.HeartbeatSeconds
	}
	if err = c.save(); err != nil {
		c.invalidate()
		return claims, err
	}
	c.publish(false, nil)
	return claims, nil
}
