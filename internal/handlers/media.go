package handlers

import (
	"net/http"
	"slices"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"dado/internal/db"
	"dado/internal/models"
)

// MediaSlots are the fixed image spots on the public site, in page order.
var MediaSlots = []string{"hero", "about", "photo", "video", "voice"}

// GET /api/media
func (h *Handler) ListMedia(w http.ResponseWriter, r *http.Request) {
	list, err := db.FindAll[models.MediaImage](r.Context(), h.db.Media, bson.M{}, bson.D{})
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	slices.SortFunc(list, func(a, b models.MediaImage) int {
		return slices.Index(MediaSlots, a.Slot) - slices.Index(MediaSlots, b.Slot)
	})
	writeJSON(w, http.StatusOK, list)
}

// PUT /api/admin/media/{slot} — replaces the image in one slot.
func (h *Handler) PutMedia(w http.ResponseWriter, r *http.Request) {
	slot := r.PathValue("slot")
	if !slices.Contains(MediaSlots, slot) {
		writeError(w, http.StatusNotFound, "Not found.")
		return
	}
	var in struct {
		URL string `json:"url"`
		Alt string `json:"alt"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	f := fieldErrors{}
	f.imageURL("url", "Image", &in.URL)
	f.text("alt", "Image description", &in.Alt, true, 200)
	if len(f) > 0 {
		writeInvalid(w, f)
		return
	}

	m := models.MediaImage{Slot: slot, URL: in.URL, Alt: in.Alt, UpdatedAt: models.Now()}
	if _, err := h.db.Media.ReplaceOne(r.Context(), bson.M{"_id": slot}, m, options.Replace().SetUpsert(true)); err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}
