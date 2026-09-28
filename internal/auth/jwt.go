package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenTTL is how long an admin stays signed in.
const TokenTTL = 24 * time.Hour

type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// IssueToken signs a token for the admin, valid for TokenTTL from now.
func IssueToken(secret []byte, adminID, email string, now time.Time) (string, error) {
	claims := Claims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   adminID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// ParseToken checks the signature and expiry. Only HS256 is accepted, which
// blocks "alg: none" and algorithm-swap tricks.
func ParseToken(secret []byte, token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims,
		func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	return claims, nil
}
