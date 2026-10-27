package licentivo

import (
	"encoding/json"
	"errors"
	"github.com/spf86/licentivo-sdk/internal/lease"
	"os"
	"path/filepath"
	"strings"
)

// Device credentials outlive a lease, including policies requiring online validation.
// Store the device credential separately from the lease so expiry, online-only policies,
// and floating-session shutdown cannot discard the credential used after a device transfer.
type credentialState struct {
	LogoutTicket     *LogoutTicket `json:"logout_ticket,omitempty"`
	Version          int           `json:"version"`
	SourceHash       string        `json:"source_hash"`
	ProductID        string        `json:"product_id"`
	Origin           string        `json:"origin"`
	DeviceID         string        `json:"device_id"`
	DeviceKey        string        `json:"device_key"`
	RequestID        string        `json:"request_id"`
	SessionID        string        `json:"session_id"`
	OfflineRequestID string        `json:"offline_request_id,omitempty"`
}

func temporaryKey(key string) bool { return strings.HasPrefix(key, "lv_tmp_") }
func (c *Client) transferred() bool {
	return c.cfg.LicenseKey != "" || c.cfg.IdentityToken != "" || c.auth.Version == 1
}
func (c *Client) cacheLicenseHash() string {
	if c.cfg.IdentityToken != "" {
		return licenseHash(c.cfg.IdentityToken)
	}
	if c.cfg.LicenseKey == "" {
		return c.auth.SourceHash
	}
	return licenseHash(c.cfg.LicenseKey)
}
func (c *Client) runtimeKey() string {
	if c.auth.DeviceKey != "" {
		return c.auth.DeviceKey
	}
	return c.cfg.LicenseKey
}
func (c *Client) loadCredential() {
	if c.cfg.CachePath == "" {
		return
	}
	if info, err := os.Stat(c.cfg.CachePath + ".credential.json"); err != nil || info.Size() > lease.MaxSize {
		return
	}
	b, err := os.ReadFile(c.cfg.CachePath + ".credential.json")
	var state credentialState
	if err != nil || len(b) > 16384 || json.Unmarshal(b, &state) != nil || state.Version != 1 || state.Origin != c.cfg.BaseURL || !lease.ValidDevice(state.SourceHash) || c.cfg.LicenseKey != "" && state.SourceHash != licenseHash(c.cfg.LicenseKey) || c.cfg.IdentityToken != "" && state.SourceHash != licenseHash(c.cfg.IdentityToken) || state.ProductID != c.cfg.ProductID || state.DeviceID != c.cfg.DeviceID {
		return
	}
	if state.DeviceKey != "" && (len(state.DeviceKey) != 50 || !strings.HasPrefix(state.DeviceKey, "lv_dev_")) {
		return
	}
	if c.cfg.LicenseKey == "" && state.DeviceKey == "" {
		return
	}
	if len(state.RequestID) != 36 || len(state.SessionID) != 36 {
		return
	}
	c.auth = state
	if state.DeviceKey == "" {
		c.sessionID = state.SessionID
	}
}
func (c *Client) prepareCredential() error {
	if !c.transferred() {
		return nil
	}
	if c.cfg.CachePath == "" {
		return errors.New("cache_path is required to retain the machine credential")
	}
	if c.auth.Version == 0 {
		c.auth = credentialState{Version: 1, SourceHash: c.cacheLicenseHash(), Origin: c.cfg.BaseURL, ProductID: c.cfg.ProductID, DeviceID: c.cfg.DeviceID, RequestID: requestUUID(), SessionID: c.sessionID}
		c.authDirty = true
	}
	if c.authDirty {
		return c.saveCredential()
	}
	return nil
}
func (c *Client) saveCredential() error {
	path := c.cfg.CachePath + ".credential.json"
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(c.auth)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credential-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(b)
	}
	if ce := tmp.Close(); err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	c.authDirty = false
	return nil
}
func (c *Client) acceptCredential(out response) error {
	// A renewed assertion for the old Launch login cannot undo explicit logout.
	// A real new login advances the server's binding generation.
	if c.cfg.LaunchConnectionID != "" && c.logoutBindings[logoutBinding(out.LogoutTicket)] {
		c.auth.LogoutTicket = out.LogoutTicket
		_ = c.denyLocked()
		return &APIError{Status: 403, Code: "LAUNCH_LOGIN_REQUIRED"}
	}
	if c.logoutJournal.SignedOut {
		c.logoutJournal.SignedOut = false
		if err := c.saveLogoutJournal(); err != nil {
			return err
		}
	}
	if out.LogoutTicket != nil {
		if err := c.prepareCredential(); err != nil {
			return err
		}
		c.auth.LogoutTicket = out.LogoutTicket
		c.authDirty = true
		if err := c.saveCredential(); err != nil {
			return err
		}
	}
	if !c.transferred() || c.auth.DeviceKey != "" {
		return nil
	}
	if len(out.DeviceKey) != 50 || !strings.HasPrefix(out.DeviceKey, "lv_dev_") {
		if !temporaryKey(c.cfg.LicenseKey) && c.cfg.IdentityToken == "" {
			return nil
		}
		return errors.New("server omitted device credential")
	}
	if err := c.prepareCredential(); err != nil {
		return err
	}
	c.auth.DeviceKey = out.DeviceKey
	c.authDirty = true
	return c.saveCredential()
}
