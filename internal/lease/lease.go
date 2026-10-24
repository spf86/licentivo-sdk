// Package lease implements the public, language-independent Licentivo Lease v1 protocol.
package lease

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"time"
)

const Prefix = "LicenovaLease/v1\n"
const MaxSize = 16384

type Envelope struct {
	Version   int    `json:"version"`
	KeyID     string `json:"key_id"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// Field order is part of the signing specification. Verify the received bytes, never re-encode them.
type Claims struct {
	Version          int            `json:"version"`
	KeyID            string         `json:"key_id"`
	TenantID         string         `json:"tenant_id"`
	ProductID        string         `json:"product_id"`
	LicenseID        string         `json:"license_id"`
	DeviceID         string         `json:"device_id"`
	ActivationID     string         `json:"activation_id"`
	Mode             string         `json:"mode"`
	Features         []string       `json:"features"`
	MaxDevices       int            `json:"max_devices"`
	IssuedAt         int64          `json:"issued_at"`
	ExpiresAt        int64          `json:"expires_at"`
	RuntimePolicy    *RuntimePolicy `json:"runtime_policy,omitempty"`
	Capabilities     *Capabilities  `json:"capabilities,omitempty"`
	OfflineRequestID string         `json:"offline_request_id,omitempty"`
}

// Optional signed fields keep legacy device-bound licenses compatible.
// The server signs extended entitlements, including session deadlines and maintenance periods.
type Capabilities struct {
	LaunchConnectionID string `json:"launch_connection_id,omitempty"`
	Environment        string `json:"environment,omitempty"`
	BindingVersion     int    `json:"binding_version,omitempty"`
	SessionID          string `json:"session_id,omitempty"`
	LicenseModel       string `json:"license_model"`
	SessionExpiresAt   int64  `json:"session_expires_at,omitempty"`
	AppVersion         string `json:"app_version,omitempty"`
	VersionReleasedAt  int64  `json:"version_released_at,omitempty"`
	MaintenanceUntil   int64  `json:"maintenance_until,omitempty"`
}

// Runtime policy is signed with the device binding. Legacy leases remain limited to 24 hours.
type RuntimePolicy struct {
	OfflineMode      string `json:"offline_mode"`
	HeartbeatSeconds int    `json:"heartbeat_seconds"`
	OfflineSeconds   int    `json:"offline_seconds"`
}

func (p RuntimePolicy) Valid() bool {
	if p.HeartbeatSeconds != 0 && (p.HeartbeatSeconds < 10 || p.HeartbeatSeconds > 86400) {
		return false
	}
	switch p.OfflineMode {
	case "permanent":
		return p.OfflineSeconds == 0
	case "none":
		return p.OfflineSeconds == 0
	case "limited":
		return p.OfflineSeconds >= 1 && p.OfflineSeconds <= 31536000
	}
	return false
}

func (c Claims) OfflineMode() string {
	if c.RuntimePolicy == nil {
		return "limited"
	}
	return c.RuntimePolicy.OfflineMode
}

func (c Claims) MaxLeaseSeconds() int64 {
	if c.RuntimePolicy == nil {
		return 86400
	}
	if !c.RuntimePolicy.Valid() {
		return 0
	}
	switch c.RuntimePolicy.OfflineMode {
	case "none":
		return int64(OnlineLeaseSeconds(c.RuntimePolicy.HeartbeatSeconds))
	case "permanent":
		return 253402300799 - c.IssuedAt
	default:
		return int64(c.RuntimePolicy.OfflineSeconds)
	}
}

// OnlineLeaseSeconds bounds a live online-only signature through timer jitter
// and a normal request round trip. It does not enable offline cache restoration.
func OnlineLeaseSeconds(heartbeat int) int {
	if heartbeat == 0 {
		return 10
	}
	return heartbeat + (heartbeat+9)/10 + 30
}

func Sign(c Claims, key ed25519.PrivateKey) (Envelope, error) {
	if len(key) != ed25519.PrivateKeySize {
		return Envelope{}, errors.New("invalid private key")
	}
	b, err := json.Marshal(c)
	if err != nil {
		return Envelope{}, err
	}
	sig := ed25519.Sign(key, append([]byte(Prefix), b...))
	return Envelope{1, c.KeyID, base64.RawURLEncoding.EncodeToString(b), base64.RawURLEncoding.EncodeToString(sig)}, nil
}

func Verify(e Envelope, keys map[string]ed25519.PublicKey, product, device string, now time.Time) (Claims, error) {
	var c Claims
	key := keys[e.KeyID]
	if e.Version != 1 || len(e.Payload) > MaxSize || len(key) != 32 {
		return c, errors.New("untrusted lease key or version")
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(e.Payload)
	sig, se := base64.RawURLEncoding.Strict().DecodeString(e.Signature)
	if err != nil || se != nil || len(sig) != 64 || len(b) > MaxSize || !ed25519.Verify(key, append([]byte(Prefix), b...), sig) {
		return c, errors.New("invalid lease signature")
	}
	if err = Decode(b, &c); err != nil {
		return c, err
	}
	if c.Version != 1 || c.KeyID != e.KeyID || c.ProductID != product || c.DeviceID != device || !validID(c.TenantID) || !validID(c.ProductID) || !validID(c.LicenseID) || !validID(c.ActivationID) || !ValidDevice(device) {
		return c, errors.New("lease context mismatch")
	}
	if c.Mode != "perpetual" && c.Mode != "timed" && c.Mode != "trial" {
		return c, errors.New("invalid lease mode")
	}
	if c.OfflineRequestID != "" && !validID(c.OfflineRequestID) {
		return c, errors.New("invalid offline request context")
	}
	if x := c.Capabilities; x != nil {
		if x.LaunchConnectionID != "" || x.Environment != "" || x.BindingVersion != 0 {
			if !validID(x.LaunchConnectionID) || x.Environment != "live" && x.Environment != "sandbox" || x.BindingVersion < 1 || x.LicenseModel != "node_locked" || !validID(x.SessionID) || x.SessionExpiresAt <= c.IssuedAt || c.ExpiresAt > x.SessionExpiresAt || c.RuntimePolicy == nil || c.RuntimePolicy.HeartbeatSeconds < 600 || c.OfflineMode() == "permanent" || c.RuntimePolicy.OfflineSeconds > 86400 {
				return c, errors.New("invalid Launch account session")
			}
		}
		if x.LicenseModel != "node_locked" && x.LicenseModel != "floating" {
			return c, errors.New("invalid license model")
		}
		if x.LicenseModel == "floating" && (!validID(x.SessionID) || x.SessionExpiresAt <= c.IssuedAt || c.ExpiresAt > x.SessionExpiresAt || c.RuntimePolicy == nil || c.RuntimePolicy.HeartbeatSeconds == 0 || c.OfflineMode() == "permanent") {
			return c, errors.New("invalid floating lease")
		}
		if x.SessionID != "" && !validID(x.SessionID) || x.SessionExpiresAt < 0 || x.SessionExpiresAt > 253402300799 || x.AppVersion != "" && !versionPattern.MatchString(x.AppVersion) || x.MaintenanceUntil < 0 || x.MaintenanceUntil > 253402300799 || x.VersionReleasedAt < 0 || x.VersionReleasedAt > c.IssuedAt || x.MaintenanceUntil > 0 && (x.AppVersion == "" || x.VersionReleasedAt == 0 || x.VersionReleasedAt > x.MaintenanceUntil) {
			return c, errors.New("invalid maintenance entitlement")
		}
	}
	if c.MaxDevices < 1 || c.MaxDevices > 10000 || c.IssuedAt <= 0 || c.ExpiresAt <= c.IssuedAt || c.ExpiresAt > 253402300799 || c.ExpiresAt-c.IssuedAt > c.MaxLeaseSeconds() || c.IssuedAt > now.Unix()+300 || now.Unix() >= c.ExpiresAt {
		return c, errors.New("expired lease or invalid clock")
	}
	if c.Features == nil || len(c.Features) > 32 || !sort.StringsAreSorted(c.Features) {
		return c, errors.New("invalid features")
	}
	for i, f := range c.Features {
		if len(f) < 1 || len(f) > 64 || (i > 0 && c.Features[i-1] == f) {
			return c, errors.New("invalid features")
		}
		for _, ch := range f {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '_') {
				return c, errors.New("invalid feature")
			}
		}
	}
	return c, nil
}

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)

func ValidDevice(s string) bool {
	b, err := hex.DecodeString(s)
	return len(s) == 64 && len(b) == 32 && err == nil && s == string(bytes.ToLower([]byte(s)))
}
func validID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	b := make([]byte, 0, 32)
	for _, c := range []byte(s) {
		if c != '-' {
			b = append(b, c)
		}
	}
	_, err := hex.DecodeString(string(b))
	return err == nil
}

// Decode rejects duplicate object keys, unknown fields, trailing JSON and oversized documents.
func Decode(b []byte, target any) error {
	if len(b) > MaxSize {
		return errors.New("JSON exceeds lease size limit")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if err := uniqueValue(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(target)
}
func uniqueValue(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	if delim, ok := t.(json.Delim); ok {
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				name, ok := k.(string)
				if !ok || seen[name] {
					return errors.New("duplicate JSON field")
				}
				seen[name] = true
				if e = uniqueValue(d); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e := uniqueValue(d); e != nil {
					return e
				}
			}
		default:
			return errors.New("invalid JSON")
		}
		_, err = d.Token()
	}
	return err
}
