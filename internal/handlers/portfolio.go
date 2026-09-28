package handlers

import (
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"

	"dado/internal/db"
	"dado/internal/models"
)

var portfolioSort = bson.D{{Key: "order", Value: 1}, {Key: "createdAt", Value: 1}}

func validatePortfolio(in *models.PortfolioFields) fieldErrors {
	f := fieldErrors{}
	f.text("title", "Title", &in.Title, true, 120)
	f.text("label", "Label", &in.Label, false, 40)
	f.text("alt", "Image description", &in.Alt, true, 200)
	f.imageURL("beforeUrl", "Before image", &in.BeforeURL)
	f.imageURL("afterUrl", "After image", &in.AfterURL)
	if in.Order < 0 {
		f.add("order", "Order can't be negative.")
	}
	return f
}

// GET /api/portfolio — active pairs only.
func (h *Handler) PublicPortfolio(w http.ResponseWriter, r *http.Request) {
	h.listPortfolio(w, r, bson.M{"active": true})
}

// GET /api/admin/portfolio
func (h *Handler) ListPortfolio(w http.ResponseWriter, r *http.Request) {
	h.listPortfolio(w, r, bson.M{})
}

func (h *Handler) listPortfolio(w http.ResponseWriter, r *http.Request, filter bson.M) {
	list, err := db.FindAll[models.PortfolioItem](r.Context(), h.db.Portfolio, filter, portfolioSort)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/admin/portfolio
func (h *Handler) CreatePortfolioItem(w http.ResponseWriter, r *http.Request) {
	in := models.PortfolioFields{Active: true}
	if !decodeJSON(w, r, &in) {
		return
	}
	if f := validatePortfolio(&in); len(f) > 0 {
		writeInvalid(w, f)
		return
	}
	now := models.Now()
	p := models.PortfolioItem{ID: bson.NewObjectID(), PortfolioFields: in, CreatedAt: now, UpdatedAt: now}
	if _, err := h.db.Portfolio.InsertOne(r.Context(), p); err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// PUT /api/admin/portfolio/{id}
func (h *Handler) UpdatePortfolioItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var p models.PortfolioItem
	if !h.load(w, r, h.db.Portfolio, id, &p) {
		return
	}
	in := p.PortfolioFields
	if !decodeJSON(w, r, &in) {
		return
	}
	if f := validatePortfolio(&in); len(f) > 0 {
		writeInvalid(w, f)
		return
	}
	p.PortfolioFields, p.UpdatedAt = in, models.Now()
	if h.save(w, r, h.db.Portfolio, id, p) {
		writeJSON(w, http.StatusOK, p)
	}
}

// DELETE /api/admin/portfolio/{id}
func (h *Handler) DeletePortfolioItem(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		h.remove(w, r, h.db.Portfolio, id)
	}
}
