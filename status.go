package licentivo

import (
	"context"
	"errors"
	"github.com/spf86/licentivo-sdk/internal/lease"
	"time"
)

// Status never makes a network request. LeaseValidUntil is the signed cache
// deadline; it is not necessarily the subscription or license expiry date.
type Status struct {
	Device          DeviceInfo `json:"device"`
	State           string     `json:"state"`
	Allowed         bool       `json:"allowed"`
	Code            string     `json:"code,omitempty"`
	RequestID       string     `json:"request_id,omitempty"`
	Retryable       bool       `json:"retryable"`
	LeaseValidUntil int64      `json:"lease_valid_until,omitempty"`
}
type localView struct {
	state          cache
	wall, timeTick time.Time
	online         bool
	failure        error
	retryAt        time.Time
}
type statusListener struct{ callback func(Status) }

func (c *Client) publish(online bool, failure error) {
	v := &localView{state: c.state, wall: c.lastWall, timeTick: c.lastTick, online: online, failure: failure}
	if e, ok := failure.(*APIError); ok && e.RetryAfterSeconds > 0 {
		v.retryAt = time.Now().Add(time.Duration(e.RetryAfterSeconds) * time.Second)
	}
	before := c.Status()
	c.view.Store(v)
	after := c.Status()
	if before != after {
		if l := c.listener.Load(); l != nil {
			go l.callback(after)
		}
	}
}
func (c *Client) OnStatusChanged(callback func(Status)) {
	if callback == nil {
		c.listener.Store(nil)
	} else {
		c.listener.Store(&statusListener{callback})
	}
}
func (c *Client) localClaims() (lease.Claims, error) {
	if c.denied.Load() {
		return lease.Claims{}, errors.New("signed out; fresh login required")
	}
	v := c.view.Load()
	if v == nil {
		return lease.Claims{}, errors.New("no valid lease")
	}
	if !v.wall.IsZero() && time.Now().Before(v.wall.Add(time.Since(v.timeTick)).Add(-5*time.Minute)) {
		return lease.Claims{}, errors.New("clock rollback")
	}
	claims, err := lease.Verify(v.state.Lease, c.keys, c.cfg.ProductID, c.cfg.DeviceID, time.Now())
	if err == nil {
		err = versionContext(claims, c.cfg.AppVersion, c.sessionID)
	}
	if err == nil {
		err = c.accountContext(claims)
	}
	return claims, err
}
func (c *Client) Status() Status {
	if c.denied.Load() {
		return Status{State: "released", Code: "LAUNCH_LOGIN_REQUIRED", Device: c.DeviceInfo()}
	}
	v := c.view.Load()
	s := Status{State: "verification_required", Device: c.DeviceInfo()}
	if v == nil {
		return s
	}
	if e, ok := v.failure.(*APIError); ok {
		s.Code = e.Code
		s.RequestID = e.RequestID
		s.Retryable = e.Retryable
	} else if v.failure != nil {
		s.Code = "NETWORK_UNAVAILABLE"
		s.Retryable = true
	}
	if claims, err := c.localClaims(); err == nil {
		s.Allowed = true
		s.LeaseValidUntil = claims.ExpiresAt
		if v.online {
			s.State = "active"
		} else {
			s.State = "offline_valid"
		}
	}
	switch s.Code {
	case "LICENSE_EXPIRED":
		s.State = "expired"
	case "LICENSE_REVOKED", "LICENSE_SUSPENDED":
		s.State = "revoked"
	case "DEVICE_RELEASED", "DEVICE_CREDENTIAL_REVOKED":
		s.State = "released"
	case "FEATURE_QUOTA_EXCEEDED":
		s.State = "quota_exhausted"
	}
	return s
}
func (c *Client) FeatureStatus(feature string) Status {
	s := c.Status()
	if !s.Allowed {
		return s
	}
	claims, err := c.localClaims()
	if err != nil {
		s.Allowed = false
		s.State = "verification_required"
		return s
	}
	for _, f := range claims.Features {
		if f == feature {
			return s
		}
	}
	s.Allowed = false
	s.Code = "FEATURE_NOT_ENTITLED"
	return s
}
func heartbeatDelay(interval int, jitter byte) time.Duration {
	return time.Duration(float64(max(600, interval)) * (1 + float64(jitter)/255*.1) * float64(time.Second))
}

// Shortened grants (for example a nearly expired login) must refresh before
// their signed deadline even when the configured heartbeat is much longer.
func heartbeatDeadlineDelay(delay, remaining time.Duration) time.Duration {
	if remaining > 0 {
		return min(delay, max(time.Second, remaining-remaining/5))
	}
	return delay
}

func authorizationRejected(err error) bool {
	if explicitRevocation(err) {
		return true
	}
	var e *APIError
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code {
	case "PRODUCT_DISABLED", "TENANT_SUSPENDED", "LICENSE_INVALID", "DEVICE_CREDENTIAL_MISMATCH", "LAUNCH_ENVIRONMENT_MISMATCH", "SESSION_EXPIRED", "VERSION_NOT_ENTITLED", "VERSION_NOT_PUBLISHED", "MAINTENANCE_EXPIRED", "MAINTENANCE_STATE_INVALID", "FEATURE_NOT_ENTITLED", "HARDWARE_ID_REQUIRED", "VIRTUAL_MACHINE_NOT_ALLOWED", "SDK_UPGRADE_REQUIRED", "LAUNCH_POLICY_UNSUPPORTED", "RUNTIME_WINDOW_INVALID":
		return true
	}
	return false
}
func waitHeartbeat(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func deniedAuthorization(err error) bool {
	if explicitRevocation(err) {
		return true
	}
	var e *APIError
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code {
	case "LICENSE_REVOKED", "LICENSE_SUSPENDED", "LICENSE_EXPIRED", "DEVICE_RELEASED", "DEVICE_CREDENTIAL_REVOKED":
		return true
	}
	return false
}

// Track startup refresh separately from the request mutex so Close can cancel
// an in-flight HTTP call without waiting to acquire its lock first.
func (c *Client) startRefresh(ctx context.Context, onError func(error)) {
	c.workersMu.Lock()
	defer c.workersMu.Unlock()
	if c.closed.Load() || c.refreshCancel != nil {
		return
	}
	ctx, c.refreshCancel = context.WithCancel(ctx)
	c.refreshDone = make(chan struct{})
	done := c.refreshDone
	go func() {
		defer close(done)
		_, err := c.RefreshOnline(ctx)
		if err != nil && ctx.Err() == nil && onError != nil {
			go onError(err)
		}
	}()
}
func (c *Client) stopRefresh() {
	c.workersMu.Lock()
	cancel, done := c.refreshCancel, c.refreshDone
	c.refreshCancel = nil
	c.workersMu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
