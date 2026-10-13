package lease

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestSharedVectors(t *testing.T) {
	b, e := os.ReadFile("../../tests/lease-vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixture struct {
		Keys    map[string]string `json:"trusted_keys"`
		Vectors []struct {
			Name     string   `json:"name"`
			Envelope Envelope `json:"envelope"`
			Product  string   `json:"product_id"`
			Device   string   `json:"device_id"`
			Now      int64    `json:"now"`
			Valid    bool     `json:"valid"`
		} `json:"vectors"`
	}
	if e = json.Unmarshal(b, &fixture); e != nil {
		t.Fatal(e)
	}
	keys := map[string]ed25519.PublicKey{}
	for id, s := range fixture.Keys {
		keys[id], _ = base64.RawURLEncoding.DecodeString(s)
	}
	for _, v := range fixture.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			_, e := Verify(v.Envelope, keys, v.Product, v.Device, time.Unix(v.Now, 0))
			if (e == nil) != v.Valid {
				t.Fatal("vector result mismatch", e)
			}
		})
	}
}
