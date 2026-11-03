package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func TestZitadelSignature(t *testing.T) {
	key, body := []byte("k"), []byte(`{"user":{"id":"1"}}`)
	now := time.Unix(1_800_000_000, 0)
	mac := hmac.New(sha256.New, key)
	_, _ = fmt.Fprintf(mac, "%d.", now.Unix())
	mac.Write(body)
	h := fmt.Sprintf("t=%d,v1=%s", now.Unix(), hex.EncodeToString(mac.Sum(nil)))
	if !validZitadelSignature(h, body, key, now) {
		t.Fatal("valid signature rejected")
	}
	if validZitadelSignature(h, []byte(`{"user":{"id":"2"}}`), key, now) {
		t.Fatal("tampered body accepted")
	}
	if validZitadelSignature(h, body, key, now.Add(10*time.Minute)) {
		t.Fatal("stale signature accepted")
	}
	if validZitadelSignature(h, body, nil, now) {
		t.Fatal("empty key must reject everything")
	}
}
