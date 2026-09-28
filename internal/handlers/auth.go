package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"dado/internal/auth"
	"dado/internal/models"
)

const resetTTL = 30 * time.Minute

const (
	wrongLogin   = "That email and password do not match."
	expiredReset = "This reset link has expired. Ask for a new one."
)

// POST /api/auth/login — sets the httpOnly session cookie (24 hours).
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	f := fieldErrors{}
	f.email("email", "Email", &in.Email)
	if in.Password == "" {
		f.add("password", "Password is required.")
	}
	if len(f) > 0 {
		writeInvalid(w, f)
		return
	}

	var a models.Admin
	err := h.db.Admins.FindOne(r.Context(), bson.M{"email": strings.ToLower(in.Email)}).Decode(&a)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		auth.CheckNothing(in.Password) // take as long as a real check
		writeError(w, http.StatusUnauthorized, wrongLogin)
		return
	case err != nil:
		writeServerError(w, r, err)
		return
	}
	if !auth.CheckPassword(a.PasswordHash, in.Password) {
		writeError(w, http.StatusUnauthorized, wrongLogin)
		return
	}

	token, err := auth.IssueToken(h.cfg.JWTSecret, a.ID.Hex(), a.Email, time.Now())
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	auth.SetSessionCookie(w, token, h.cfg.CookieSecure)
	writeJSON(w, http.StatusOK, models.AdminUser{ID: a.ID.Hex(), Email: a.Email})
}

// POST /api/auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	auth.ClearSessionCookie(w, h.cfg.CookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/auth/me (behind RequireAdmin)
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, auth.AdminFrom(r.Context()))
}

// POST /api/auth/forgot — always answers {ok:true} straight away, and does
// the lookup and email in the background. The reply looks and takes the same
// whether or not the email belongs to an admin, so it can't be used to find out.
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	f := fieldErrors{}
	f.email("email", "Email", &in.Email)
	if len(f) > 0 {
		writeInvalid(w, f)
		return
	}

	email := strings.ToLower(in.Email)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Minute)
	h.mail.Later(func() {
		defer cancel()
		if err := h.startReset(ctx, email); err != nil {
			slog.Error("password reset not started", "err", err)
		}
	})
	writeJSON(w, http.StatusOK, okBody)
}

// startReset stores a hashed one-time token for the admin with this email and
// sends the link. Unknown emails are ignored.
func (h *Handler) startReset(ctx context.Context, email string) error {
	var a models.Admin
	err := h.db.Admins.FindOne(ctx, bson.M{"email": email}).Decode(&a)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := hex.EncodeToString(raw)
	if _, err := h.db.Admins.UpdateOne(ctx, bson.M{"_id": a.ID}, bson.M{"$set": bson.M{
		"resetTokenHash": auth.HashToken(token),
		"resetExpires":   models.Now().Add(resetTTL),
	}}); err != nil {
		return err
	}
	msg, err := h.mail.PasswordReset(a.Email, h.cfg.SiteURL+"/admin/reset-password?token="+token)
	if err != nil {
		return err
	}
	return h.mail.Send(msg)
}

// POST /api/auth/reset — sets a new password with a token from the email.
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	switch n := utf8.RuneCountInString(in.Password); {
	case n < 8:
		writeInvalid(w, fieldErrors{"password": "Use at least 8 characters."})
		return
	case n > 128:
		writeInvalid(w, fieldErrors{"password": "That password is too long."})
		return
	}

	if in.Token == "" {
		writeError(w, http.StatusBadRequest, expiredReset)
		return
	}
	// Check the token before hashing: bcrypt is deliberately slow, and only a
	// real reset link should be able to make the server spend that time.
	tokenHash := auth.HashToken(in.Token)
	var a models.Admin
	err := h.db.Admins.FindOne(r.Context(), bson.M{
		"resetTokenHash": tokenHash, "resetExpires": bson.M{"$gt": models.Now()},
	}).Decode(&a)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		writeError(w, http.StatusBadRequest, expiredReset)
		return
	case err != nil:
		writeServerError(w, r, err)
		return
	}

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	// Set the password and use up the token in one step, so a link works once
	// even if it is submitted twice at the same moment.
	res, err := h.db.Admins.UpdateOne(r.Context(), bson.M{
		"_id": a.ID, "resetTokenHash": tokenHash, "resetExpires": bson.M{"$gt": models.Now()},
	}, bson.M{
		"$set":   bson.M{"passwordHash": hash},
		"$unset": bson.M{"resetTokenHash": "", "resetExpires": ""},
	})
	switch {
	case err != nil:
		writeServerError(w, r, err)
	case res.MatchedCount == 0:
		writeError(w, http.StatusBadRequest, expiredReset)
	default:
		writeJSON(w, http.StatusOK, okBody)
	}
}
