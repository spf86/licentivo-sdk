package licentivo

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductInitializationDefaults(t *testing.T) {
	client, err := New(Config{
		ProductID:   "923c787a-0ddb-43b8-8cc9-b27620750330",
		TrustedKeys: map[string]string{"product-key": base64.RawURLEncoding.EncodeToString(make([]byte, 32))},
		LicenseKey:  "lv_lic_" + strings.Repeat("a", 43), CachePath: filepath.Join(t.TempDir(), "lease.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.cfg.BaseURL != "https://www.licentivo.com" {
		t.Fatal("ordinary product startup did not use the official service")
	}
	if client.cfg.AppVersion != "" {
		t.Fatal("ordinary product startup must not require an artificial application version")
	}
}
