package licentivo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// LogoutTicket can only release this incarnation of one device binding.
// It contains no credential capable of activation, validation or protected use.
type LogoutTicket struct {
	OperationID        string `json:"operation_id"`
	ProductID          string `json:"product_id"`
	ActivationID       string `json:"activation_id"`
	ExpectedGeneration int    `json:"expected_generation"`
	ReleaseToken       string `json:"release_token"`
}
type LogoutResult struct {
	LocalComplete     bool
	RevocationPending bool
}
type logoutJournal struct {
	Version          int            `json:"version"`
	Revision         int64          `json:"revision,omitempty"`
	SourceHash       string         `json:"source_hash"`
	BindingKey       string         `json:"binding_key,omitempty"`
	SignedOut        bool           `json:"signed_out"`
	Pending          []LogoutTicket `json:"pending"`
	OfflineRequestID string         `json:"offline_request_id,omitempty"`
}

func explicitRevocation(err error) bool {
	var e *APIError
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code {
	case "LICENSE_REVOKED", "LICENSE_SUSPENDED", "LICENSE_EXPIRED", "DEVICE_RELEASED", "DEVICE_NOT_ACTIVE", "DEVICE_CREDENTIAL_REVOKED", "DEVICE_CREDENTIAL_INVALID", "AUTHORIZATION_SESSION_REVOKED", "AUTHORIZATION_SESSION_EXPIRED", "LAUNCH_LOGIN_REQUIRED", "LAUNCH_ENTITLEMENT_REVOKED", "LAUNCH_ENTITLEMENT_EXPIRED":
		return true
	}
	return false
}
func (c *Client) loadLogoutJournal() {
	c.logoutJournal = logoutJournal{Version: 1}
	c.logoutBindings = map[string]bool{}
	records := map[string]logoutJournal{}
	read := func(path string) {
		b, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return
		}
		var j logoutJournal
		if err != nil || len(b) > 65536 || json.Unmarshal(b, &j) != nil || j.Version != 1 {
			c.denied.Store(true)
			return
		}
		scope := j.SourceHash + "\n" + j.BindingKey
		old, exists := records[scope]
		if !exists || j.Revision > old.Revision || j.Revision == old.Revision && (j.SignedOut || !old.SignedOut) {
			records[scope] = j
		}
	}
	read(c.cfg.CachePath + ".logout.json")
	if dir, err := c.logoutDirectory(); err == nil {
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) {
			c.denied.Store(true)
		}
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) == ".json" {
				read(filepath.Join(dir, entry.Name()))
			}
		}
	}
	source, binding := c.cacheLicenseHash(), logoutBinding(c.auth.LogoutTicket)
	pending := map[string]bool{}
	tickets := []LogoutTicket{}
	for _, j := range records {
		if j.SignedOut && j.BindingKey != "" {
			c.logoutBindings[j.BindingKey] = true
		}
		if (j.SourceHash == source || c.cfg.LaunchConnectionID != "" && binding != "" && j.BindingKey == binding || c.auth.Version == 0 && c.cfg.IdentityToken == "" && c.cfg.LicenseKey == "") && (j.BindingKey == "" || binding == "" || j.BindingKey == binding) {
			c.logoutJournal = j
			if j.SignedOut && c.cfg.LicenseKey == "" {
				c.denied.Store(true)
			}
		}
		for _, ticket := range j.Pending {
			if !pending[ticket.OperationID] {
				pending[ticket.OperationID] = true
				tickets = append(tickets, ticket)
			}
		}
	}
	c.logoutJournal.Pending = tickets
}
func logoutBinding(ticket *LogoutTicket) string {
	if ticket == nil {
		return ""
	}
	return ticket.ActivationID + ":" + strconv.Itoa(ticket.ExpectedGeneration)
}
func (c *Client) logoutDirectory() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(c.cfg.CachePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "Licentivo", "logout", licenseHash(path+"\n"+c.cfg.BaseURL+"\n"+c.cfg.ProductID+"\n"+c.cfg.DeviceID)), nil
}
func writeLogoutJournal(path string, j logoutJournal) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".logout-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (c *Client) saveLogoutJournal() error {
	c.logoutJournal.Revision = max(c.logoutJournal.Revision+1, time.Now().UnixMilli())
	// Independent, binding-scoped records survive a locked/custom cache directory
	// and cannot be overwritten by an older instance using the same cache.
	dir, fallbackErr := c.logoutDirectory()
	if fallbackErr == nil {
		fallbackErr = writeLogoutJournal(filepath.Join(dir, licenseHash(c.logoutJournal.SourceHash+"\n"+c.logoutJournal.BindingKey)+".json"), c.logoutJournal)
	}
	primaryErr := writeLogoutJournal(c.cfg.CachePath+".logout.json", c.logoutJournal)
	if fallbackErr == nil || c.logoutJournal.SignedOut && primaryErr == nil {
		return nil
	}
	return errors.Join(primaryErr, fallbackErr)
}
func (c *Client) acknowledgeLogout(operation string) error {
	dir, err := c.logoutDirectory()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var j logoutJournal
		if len(b) > 65536 || json.Unmarshal(b, &j) != nil || j.Version != 1 {
			return errors.New("invalid logout journal")
		}
		pending := j.Pending[:0]
		changed := false
		for _, ticket := range j.Pending {
			if ticket.OperationID == operation {
				changed = true
			} else {
				pending = append(pending, ticket)
			}
		}
		if changed {
			j.Pending = pending
			if err = writeLogoutJournal(path, j); err != nil {
				return err
			}
		}
	}
	return nil
}
func (c *Client) denyLocked() error {
	return c.denyBindingLocked(true)
}

// Offline release exports carry their own revocation request. Persist the local
// barrier without sending a second request that could invalidate the export.
func (c *Client) denyBindingLocked(queueRelease bool) error {
	c.denied.Store(true)
	c.logoutJournal.Version = 1
	if source := c.cacheLicenseHash(); source != "" && source != licenseHash("") {
		c.logoutJournal.SourceHash = source
	}
	c.logoutJournal.SignedOut = true
	c.logoutJournal.OfflineRequestID = ""
	if binding := logoutBinding(c.auth.LogoutTicket); binding != "" {
		c.logoutJournal.BindingKey = binding
		if c.logoutBindings == nil {
			c.logoutBindings = map[string]bool{}
		}
		c.logoutBindings[binding] = true
	}
	if queueRelease && c.auth.LogoutTicket != nil {
		ticket := *c.auth.LogoutTicket
		found := false
		for _, p := range c.logoutJournal.Pending {
			if p.OperationID == ticket.OperationID {
				found = true
			}
		}
		if !found {
			c.logoutJournal.Pending = append(c.logoutJournal.Pending, ticket)
		}
	}
	err := c.saveLogoutJournal()
	c.invalidate()
	c.auth = credentialState{}
	c.cfg.LicenseKey = ""
	c.cfg.IdentityToken = ""
	c.authDirty = false
	if len(c.logoutJournal.Pending) > 0 {
		c.startLogoutRecovery()
	}
	return err
}

// Logout denies protected work before waiting for any in-flight HTTP request.
// A network failure returns local completion with a durable unlink-only retry.
func (c *Client) Logout(ctx context.Context) (LogoutResult, error) {
	c.denied.Store(true)
	c.mu.Lock()
	err := c.denyLocked()
	if c.cancel != nil {
		c.cancel()
	}
	c.publish(false, &APIError{Code: "DEVICE_RELEASED"})
	c.mu.Unlock()
	retryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	_ = c.RetryPendingLogouts(retryCtx)
	cancel()
	c.mu.Lock()
	pending := len(c.logoutJournal.Pending) > 0
	c.mu.Unlock()
	if pending {
		c.startLogoutRecovery()
	}
	return LogoutResult{LocalComplete: err == nil, RevocationPending: pending}, err
}
func (c *Client) RetryPendingLogouts(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.logoutJournal.Pending) > 0 {
		ticket := c.logoutJournal.Pending[0]
		b, _ := json.Marshal(ticket)
		req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.BaseURL+"/api/v1/sessions/logout", bytes.NewReader(b))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, 65537))
		res.Body.Close()
		if err != nil {
			return err
		}
		var envelope struct {
			Data struct {
				Status       string `json:"status"`
				OperationID  string `json:"operation_id"`
				ActivationID string `json:"activation_id"`
				Generation   int    `json:"expected_generation"`
			} `json:"data"`
		}
		if res.StatusCode != 200 || len(raw) > 65536 || json.Unmarshal(raw, &envelope) != nil || envelope.Data.Status != "succeeded" && envelope.Data.Status != "stale" || envelope.Data.OperationID != ticket.OperationID || envelope.Data.ActivationID != ticket.ActivationID || envelope.Data.Generation != ticket.ExpectedGeneration {
			return errors.New("device revocation remains pending")
		}
		c.logoutJournal.Pending = c.logoutJournal.Pending[1:]
		if err = c.acknowledgeLogout(ticket.OperationID); err != nil {
			return err
		}
		if err = c.saveLogoutJournal(); err != nil {
			return err
		}
	}
	return nil
}
func (c *Client) startLogoutRecovery() {
	if !c.retryingLogout.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer c.retryingLogout.Store(false)
		for delay := time.Second; !c.closed.Load(); delay = min(delay*2, 5*time.Minute) {
			timer := time.NewTimer(delay)
			<-timer.C
			if c.closed.Load() {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := c.RetryPendingLogouts(ctx)
			cancel()
			if err == nil {
				return
			}
		}
	}()
}
