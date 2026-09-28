// Package handlers turns HTTP requests into database work. One file per
// resource; routes live in internal/routes.
package handlers

import (
	"errors"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"dado/internal/config"
	"dado/internal/db"
	"dado/internal/mail"
)

type Handler struct {
	db   *db.DB
	cfg  *config.Config
	mail *mail.Mailer
}

func New(d *db.DB, cfg *config.Config, m *mail.Mailer) *Handler {
	return &Handler{db: d, cfg: cfg, mail: m}
}

// GET /api/health
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, okBody)
}

// load fetches the record with this _id into dst. On failure it has already
// written the 404 or 500 response and returns false.
func (h *Handler) load(w http.ResponseWriter, r *http.Request, coll *mongo.Collection, id any, dst any) bool {
	err := coll.FindOne(r.Context(), bson.M{"_id": id}).Decode(dst)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		writeError(w, http.StatusNotFound, "Not found.")
		return false
	case err != nil:
		writeServerError(w, r, err)
		return false
	}
	return true
}

// save replaces the whole record. Handlers load, change and save, which keeps
// every update the same shape.
// ponytail: last write wins; fine for one admin, add a version field if several
// people start editing the same records at once.
func (h *Handler) save(w http.ResponseWriter, r *http.Request, coll *mongo.Collection, id any, doc any) bool {
	if _, err := coll.ReplaceOne(r.Context(), bson.M{"_id": id}, doc); err != nil {
		writeServerError(w, r, err)
		return false
	}
	return true
}

// remove deletes the record with this _id; 404 when there was nothing to delete.
func (h *Handler) remove(w http.ResponseWriter, r *http.Request, coll *mongo.Collection, id any) {
	res, err := coll.DeleteOne(r.Context(), bson.M{"_id": id})
	switch {
	case err != nil:
		writeServerError(w, r, err)
	case res.DeletedCount == 0:
		writeError(w, http.StatusNotFound, "Not found.")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
