package handlers

import (
	"fmt"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"

	"dado/internal/db"
	"dado/internal/models"
)

// photo, video, voice happen to sort alphabetically, so discipline then order.
var serviceSort = bson.D{{Key: "discipline", Value: 1}, {Key: "order", Value: 1}, {Key: "name", Value: 1}}

func validateService(in *models.ServiceFields) fieldErrors {
	f := fieldErrors{}
	f.text("name", "Name", &in.Name, true, 120)
	f.oneOf("discipline", in.Discipline, "Choose photo, video or voice.", disciplines...)
	f.text("group", "Group", &in.Group, true, 80)
	f.text("use", "Typical use", &in.Use, false, 160)
	f.money("rate", "Enter a price of $0 or more.", &in.Rate)
	f.text("unit", "Unit", &in.Unit, true, 40)
	f.text("turnaround", "Turnaround", &in.Turnaround, false, 40)
	if in.Order < 0 {
		f.add("order", "Order can't be negative.")
	}
	return f
}

// GET /api/services — what the public site shows.
func (h *Handler) PublicServices(w http.ResponseWriter, r *http.Request) {
	h.listServices(w, r, bson.M{"active": true})
}

// GET /api/admin/services — everything, including hidden ones.
func (h *Handler) ListServices(w http.ResponseWriter, r *http.Request) {
	h.listServices(w, r, bson.M{})
}

func (h *Handler) listServices(w http.ResponseWriter, r *http.Request, filter bson.M) {
	list, err := db.FindAll[models.Service](r.Context(), h.db.Services, filter, serviceSort)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/admin/services
func (h *Handler) CreateService(w http.ResponseWriter, r *http.Request) {
	var in models.ServiceFields
	if !decodeJSON(w, r, &in) {
		return
	}
	if f := validateService(&in); len(f) > 0 {
		writeInvalid(w, f)
		return
	}
	now := models.Now()
	s := models.Service{ID: bson.NewObjectID(), ServiceFields: in, CreatedAt: now, UpdatedAt: now}
	if _, err := h.db.Services.InsertOne(r.Context(), s); err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s)
}

// PUT /api/admin/services/{id}
func (h *Handler) UpdateService(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var s models.Service
	if !h.load(w, r, h.db.Services, id, &s) {
		return
	}
	in := s.ServiceFields
	if !decodeJSON(w, r, &in) {
		return
	}
	if f := validateService(&in); len(f) > 0 {
		writeInvalid(w, f)
		return
	}
	s.ServiceFields, s.UpdatedAt = in, models.Now()
	if h.save(w, r, h.db.Services, id, s) {
		writeJSON(w, http.StatusOK, s)
	}
}

// DELETE /api/admin/services/{id}
// Refused while jobs point at the service: deleting it would blank out billing
// history. Hiding it (active=false) is the safe way to retire a service.
func (h *Handler) DeleteService(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	n, err := h.db.Jobs.CountDocuments(r.Context(), bson.M{"serviceId": id})
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if n > 0 {
		msg := fmt.Sprintf("%d jobs use this service — hide it instead.", n)
		if n == 1 {
			msg = "1 job uses this service — hide it instead."
		}
		writeError(w, http.StatusConflict, msg)
		return
	}
	h.remove(w, r, h.db.Services, id)
}
