package handlers

import (
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"

	"dado/internal/db"
	"dado/internal/models"
)

func validateClient(in *models.ClientFields) fieldErrors {
	f := fieldErrors{}
	f.text("name", "Name", &in.Name, true, 100)
	f.text("company", "Company", &in.Company, false, 120)
	f.email("email", "Email", &in.Email)
	f.phone("phone", &in.Phone)
	f.text("country", "Country", &in.Country, false, 60)
	f.text("notes", "Notes", &in.Notes, false, 2000)
	if in.Since == "" {
		in.Since = models.Today()
	}
	f.date("since", &in.Since)
	if in.Source == "" {
		in.Source = "direct"
	}
	f.oneOf("source", in.Source, "Source must be trial or direct.", "trial", "direct")
	return f
}

// GET /api/admin/clients
func (h *Handler) ListClients(w http.ResponseWriter, r *http.Request) {
	list, err := db.FindAll[models.Client](r.Context(), h.db.Clients, bson.M{}, bson.D{{Key: "createdAt", Value: -1}})
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/admin/clients
func (h *Handler) CreateClient(w http.ResponseWriter, r *http.Request) {
	var in models.ClientFields
	if !decodeJSON(w, r, &in) {
		return
	}
	if f := validateClient(&in); len(f) > 0 {
		writeInvalid(w, f)
		return
	}
	c := models.Client{ID: bson.NewObjectID(), ClientFields: in, CreatedAt: models.Now()}
	if _, err := h.db.Clients.InsertOne(r.Context(), c); err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// PUT /api/admin/clients/{id} — `since` and `source` keep their values when left out.
func (h *Handler) UpdateClient(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var c models.Client
	if !h.load(w, r, h.db.Clients, id, &c) {
		return
	}
	in := c.ClientFields
	if !decodeJSON(w, r, &in) {
		return
	}
	if f := validateClient(&in); len(f) > 0 {
		writeInvalid(w, f)
		return
	}
	c.ClientFields = in
	if h.save(w, r, h.db.Clients, id, c) {
		writeJSON(w, http.StatusOK, c)
	}
}

// DELETE /api/admin/clients/{id}
// Also deletes the client's jobs and turns any trial that became this client
// back into an open request.
// ponytail: three separate writes, no transaction. Each step is safe to
// repeat, so a failure halfway is fixed by deleting again.
func (h *Handler) DeleteClient(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var c models.Client
	if !h.load(w, r, h.db.Clients, id, &c) {
		return
	}
	ctx := r.Context()
	if _, err := h.db.Trials.UpdateMany(ctx, bson.M{"clientId": id},
		bson.M{"$set": bson.M{"clientId": nil, "status": "new", "flagReason": ""}}); err != nil {
		writeServerError(w, r, err)
		return
	}
	if _, err := h.db.Jobs.DeleteMany(ctx, bson.M{"clientId": id}); err != nil {
		writeServerError(w, r, err)
		return
	}
	h.remove(w, r, h.db.Clients, id)
}
