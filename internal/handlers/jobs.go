package handlers

import (
	"context"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"dado/internal/db"
	"dado/internal/models"
)

var jobStatuses = []string{"quoted", "in-progress", "delivered"}

// validateJob checks the whole job, including that its client and service exist.
func (h *Handler) validateJob(ctx context.Context, in *models.JobFields) (fieldErrors, error) {
	f := fieldErrors{}
	for _, ref := range []struct {
		field   string
		id      bson.ObjectID
		coll    *mongo.Collection
		message string
	}{
		{"clientId", in.ClientID, h.db.Clients, "Choose a client."},
		{"serviceId", in.ServiceID, h.db.Services, "Pick a service."},
	} {
		if ref.id.IsZero() {
			f.add(ref.field, ref.message)
			continue
		}
		n, err := ref.coll.CountDocuments(ctx, bson.M{"_id": ref.id})
		if err != nil {
			return nil, err
		}
		if n == 0 {
			f.add(ref.field, ref.message)
		}
	}

	if in.Date == "" {
		in.Date = models.Today()
	}
	f.date("date", &in.Date)
	if in.Qty < 1 {
		f.add("qty", "Quantity must be at least 1.")
	}
	f.money("amount", "Enter what you are charging.", &in.Amount)
	f.money("paid", "Enter an amount of $0 or more.", &in.Paid)
	if !f.has("amount") && !f.has("paid") && in.Paid > in.Amount {
		f.add("paid", "Paid cannot exceed the amount charged.")
	}
	f.oneOf("status", in.Status, "Choose a status.", jobStatuses...)
	f.text("details", "Details", &in.Details, false, 2000)
	f.optionalLink("link", &in.Link)
	return f, nil
}

// GET /api/admin/jobs?clientId=
func (h *Handler) ListJobs(w http.ResponseWriter, r *http.Request) {
	filter := bson.M{}
	if v := r.URL.Query().Get("clientId"); v != "" {
		id, err := bson.ObjectIDFromHex(v)
		if err != nil {
			writeJSON(w, http.StatusOK, []models.Job{})
			return
		}
		filter["clientId"] = id
	}
	list, err := db.FindAll[models.Job](r.Context(), h.db.Jobs, filter,
		bson.D{{Key: "date", Value: -1}, {Key: "createdAt", Value: -1}})
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/admin/jobs
func (h *Handler) CreateJob(w http.ResponseWriter, r *http.Request) {
	in := models.JobFields{Status: "quoted"}
	if !decodeJSON(w, r, &in) {
		return
	}
	f, err := h.validateJob(r.Context(), &in)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if len(f) > 0 {
		writeInvalid(w, f)
		return
	}
	j := models.Job{ID: bson.NewObjectID(), JobFields: in, CreatedAt: models.Now()}
	if _, err := h.db.Jobs.InsertOne(r.Context(), j); err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, j)
}

// PATCH /api/admin/jobs/{id} — send only what changed (e.g. {"paid": 120}).
func (h *Handler) PatchJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var j models.Job
	if !h.load(w, r, h.db.Jobs, id, &j) {
		return
	}
	in := j.JobFields
	if !decodeJSON(w, r, &in) {
		return
	}
	f, err := h.validateJob(r.Context(), &in)
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if len(f) > 0 {
		writeInvalid(w, f)
		return
	}
	j.JobFields = in
	if h.save(w, r, h.db.Jobs, id, j) {
		writeJSON(w, http.StatusOK, j)
	}
}

// DELETE /api/admin/jobs/{id}
func (h *Handler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		h.remove(w, r, h.db.Jobs, id)
	}
}
