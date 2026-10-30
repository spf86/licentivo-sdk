package licentivo

import (
	"strings"
	"unicode"
)

// Display data only; changing a name never changes the licensing identity.
type DeviceInfo struct {
	Name       string `json:"name"`
	Code       string `json:"code"`
	Platform   string `json:"platform"`
	OSVersion  string `json:"os_version"`
	CPU        string `json:"cpu"`
	MemoryMB   int64  `json:"memory_mb"`
	GPU        string `json:"gpu"`
	SystemType string `json:"system_type"`
	StorageGB  int64  `json:"storage_gb"`
}

func displayDeviceName(name, platform string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[:120])
	}
	if name == "" {
		name = map[string]string{"windows": "Windows", "darwin": "macOS", "linux": "Linux"}[platform]
	}
	return name
}

func (c *Client) DeviceInfo() DeviceInfo {
	code := ""
	if len(c.cfg.DeviceID) == 64 {
		code = strings.ToUpper(c.cfg.DeviceID[:4] + "-" + c.cfg.DeviceID[60:])
	}
	info := c.deviceDetails
	info.Name = c.cfg.DeviceName
	info.Code = code
	info.Platform = c.profile.Platform
	return info
}
