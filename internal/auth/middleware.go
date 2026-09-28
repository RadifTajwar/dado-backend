package auth

import (
	"context"
	"encoding/json"
	"net/http"

	"dado/internal/models"
)

// CookieName carries the admin JWT. It is httpOnly, so page scripts can't read it.
const CookieName = "dado_token"

type ctxKey struct{}

// RequireAdmin lets a request through only with a valid, unexpired token and
// puts the signed-in admin in the request context.
func RequireAdmin(secret []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieName)
		if err != nil {
			unauthorized(w)
			return
		}
		claims, err := ParseToken(secret, cookie.Value)
		if err != nil {
			unauthorized(w)
			return
		}
		// ponytail: a token stays valid until it expires, even after a password
		// reset; add a passwordChangedAt check here if that ever matters.
		admin := models.AdminUser{ID: claims.Subject, Email: claims.Email}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, admin)))
	})
}

// AdminFrom returns the admin that RequireAdmin put in the context.
func AdminFrom(ctx context.Context) models.AdminUser {
	admin, _ := ctx.Value(ctxKey{}).(models.AdminUser)
	return admin
}

// SetSessionCookie stores the token for TokenTTL. SameSite=Lax keeps the
// cookie off cross-site POSTs, which is what stops CSRF here.
func SetSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(TokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": "Please sign in again."})
}
