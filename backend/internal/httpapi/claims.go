package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ZitadelClaims is the target of a Zitadel Actions v2 function execution (preuserinfo and
// preaccesstoken). It appends a "groups" claim with the user's Malgary groups, which NetBird
// reads through JWT group sync. The backend stays the system of record for groups (Principle X).
// Request format verified in pilot test P-3.
func (h *Handlers) ZitadelClaims(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil || !validZitadelSignature(r.Header.Get("ZITADEL-Signature"), body, h.ClaimsSigningKey, time.Now()) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	var req struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		UserInfo struct {
			Sub string `json:"sub"`
		} `json:"user_info"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	uid := req.User.ID
	if uid == "" {
		uid = req.UserInfo.Sub
	}
	groups := []string{}
	if uid != "" && h.Users != nil {
		g, err := h.Users.GroupsForSignInUser(r.Context(), uid)
		if err != nil {
			// Unknown users (e.g. operators in the admin org) get no groups rather than an error.
			slog.DebugContext(r.Context(), "no groups for sign-in user", "user", uid, "err", err)
		} else {
			groups = g
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"append_claims": []map[string]any{{"key": "groups", "value": groups}},
	})
}

// validZitadelSignature checks "t=<unix>,v1=<hex hmac-sha256(key, t + "." + body)>" with a
// five-minute tolerance.
func validZitadelSignature(header string, body, key []byte, now time.Time) bool {
	if len(key) == 0 {
		return false
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || now.Sub(time.Unix(sec, 0)).Abs() > 5*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, s := range sigs {
		got, err := hex.DecodeString(s)
		if err == nil && hmac.Equal(got, want) {
			return true
		}
	}
	return false
}
