package tenancy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// sessionTokenPrefix marks a MockAgents session token, distinguishing it from
// an API key (mak_) in logs and code.
const sessionTokenPrefix = "mas_"

// generateSessionToken returns an opaque session token and its SHA-256 hash.
// Only the hash is persisted; the token is handed to the browser once. 32 bytes
// = 256 bits of entropy, so the token is unguessable and the hash is the lookup
// key (a DB dump never reveals a usable token).
func generateSessionToken() (token, hash string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	token = sessionTokenPrefix + base64.RawURLEncoding.EncodeToString(b[:])
	return token, hashToken(token), nil
}

// hashToken returns the hex SHA-256 of a session token — the value stored and
// looked up. Constant-format so generate and resolve cannot drift.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
