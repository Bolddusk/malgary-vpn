package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// idempotencyKey derives a stable key from the request, so re-applying the same file is a
// no-op on the server (FR-015).
func idempotencyKey(body []byte) string {
	s := sha256.Sum256(body)
	return hex.EncodeToString(s[:16])
}

func printJSON(b []byte) error {
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return err
	}
	fmt.Println(out.String())
	return nil
}
