// Package auth handles admin passwords, the JWT session cookie and rate limits.
package auth

import (
	"crypto/sha256"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

// bcrypt only reads the first 72 bytes of a password. Hashing it with SHA-256
// first gives every password (up to our 128-character limit) the same fixed
// 64-byte input, so long passwords are neither rejected nor truncated.
func prehash(password string) []byte {
	sum := sha256.Sum256([]byte(password))
	return []byte(hex.EncodeToString(sum[:]))
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword(prehash(password), bcrypt.DefaultCost)
	return string(hash), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), prehash(password)) == nil
}

// dummyHash lets a login for an unknown email take as long as a real one, so
// response time does not reveal which emails have an account.
var dummyHash, _ = HashPassword("dado-timing-equaliser")

// CheckNothing burns the same bcrypt time as CheckPassword and always fails.
func CheckNothing(password string) bool {
	CheckPassword(dummyHash, password)
	return false
}

// HashToken is for reset tokens: they are long and random, so a fast SHA-256
// is enough, and only the hash is stored.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
