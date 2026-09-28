package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"go.mongodb.org/mongo-driver/v2/bson"

	"dado/internal/db"
	"dado/internal/models"
)

// Statuses an admin can set by hand; "converted" only comes from Convert.
var trialStatuses = []string{"new", "declined", "flagged"}

// trialInput is the public free-trial form.
type trialInput struct {
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Company  string   `json:"company"`
	Phone    string   `json:"phone"`
	Services []string `json:"services"`
	Link     string   `json:"link"`
	Volume   string   `json:"volume"`
	Deadline string   `json:"deadline"`
	Brief    string   `json:"brief"`
	NDA      bool     `json:"nda"`
	Website  string   `json:"website"` // honeypot
}

func (in *trialInput) validate() fieldErrors {
	f := fieldErrors{}
	f.text("name", "Full name", &in.Name, true, 100)
	f.email("email", "Work email", &in.Email)
	f.text("company", "Company", &in.Company, false, 120)
	f.phone("phone", &in.Phone)

	// keep each known service once, in the usual order; anything else is an error
	var picked []string
	for _, d := range disciplines {
		if slices.Contains(in.Services, d) {
			picked = append(picked, d)
		}
	}
	unknown := slices.ContainsFunc(in.Services, func(s string) bool { return !slices.Contains(disciplines, s) })
	switch {
	case len(in.Services) == 0:
		f.add("services", "Pick at least one service.")
	case unknown:
		f.add("services", "Choose photo, video or voice.")
	}
	in.Services = picked

	f.cloudLink("link", &in.Link)
	f.text("volume", "Volume", &in.Volume, false, 60)
	f.text("deadline", "Deadline", &in.Deadline, false, 80)
	f.detail("brief", "The brief", &in.Brief)
	return f
}

// POST /api/trials
func (h *Handler) CreateTrial(w http.ResponseWriter, r *http.Request) {
	var in trialInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Website != "" { // a bot: say thanks and do nothing
		writeJSON(w, http.StatusCreated, okBody)
		return
	}
	if f := in.validate(); len(f) > 0 {
		writeInvalid(w, f)
		return
	}

	t := models.Trial{
		ID: bson.NewObjectID(), Name: in.Name, Email: in.Email, Company: in.Company, Phone: in.Phone,
		Services: in.Services, Link: in.Link, Volume: in.Volume, Deadline: in.Deadline,
		Brief: in.Brief, NDA: in.NDA, Status: "new", CreatedAt: models.Now(),
	}
	if _, err := h.db.Trials.InsertOne(r.Context(), t); err != nil {
		writeServerError(w, r, err)
		return
	}

	// Saved first; emails can fail without losing the request.
	if alert, receipt, err := h.mail.TrialEmails(t); err != nil {
		slog.Error("trial emails not built", "err", err)
	} else {
		h.mail.Go(alert, receipt)
	}
	writeJSON(w, http.StatusCreated, okBody)
}

// GET /api/admin/trials
func (h *Handler) ListTrials(w http.ResponseWriter, r *http.Request) {
	list, err := db.FindAll[models.Trial](r.Context(), h.db.Trials, bson.M{}, bson.D{{Key: "createdAt", Value: -1}})
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// PATCH /api/admin/trials/{id}
func (h *Handler) PatchTrial(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var t models.Trial
	if !h.load(w, r, h.db.Trials, id, &t) {
		return
	}
	var p statusPatch
	if !decodeJSON(w, r, &p) {
		return
	}
	if p.empty() {
		writeError(w, http.StatusBadRequest, "Nothing to update.")
		return
	}
	if f := p.apply(&t.Status, &t.FlagReason, &t.Read, trialStatuses); len(f) > 0 {
		writeInvalid(w, f)
		return
	}
	if t.ClientID != nil && t.Status == "new" { // unflagging a converted trial: it is still a client
		t.Status = "converted"
	}
	if h.save(w, r, h.db.Trials, id, t) {
		writeJSON(w, http.StatusOK, t)
	}
}

// POST /api/admin/trials/{id}/convert
// Creates a client from the trial. The trial keeps a pointer to the client,
// so the same trial can never be converted twice.
func (h *Handler) ConvertTrial(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var t models.Trial
	if !h.load(w, r, h.db.Trials, id, &t) {
		return
	}

	c := models.Client{
		ID: bson.NewObjectID(),
		ClientFields: models.ClientFields{
			Name: t.Name, Company: t.Company, Email: t.Email, Phone: t.Phone,
			Since: models.Today(), Source: "trial", Notes: "Converted from a free-trial request.",
		},
		CreatedAt: models.Now(),
	}

	// Claim the trial in one atomic step before creating anything: only one
	// request can move clientId from null to a client, so a double click or
	// two admins at once can't produce two clients.
	res, err := h.db.Trials.UpdateOne(r.Context(),
		bson.M{"_id": id, "clientId": nil},
		bson.M{"$set": bson.M{"clientId": c.ID, "status": "converted", "flagReason": ""}})
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if res.MatchedCount == 0 {
		writeError(w, http.StatusConflict, "This trial is already a client.")
		return
	}

	if _, err := h.db.Clients.InsertOne(r.Context(), c); err != nil {
		// Give the claim back so the admin can simply try again. WithoutCancel:
		// the undo must run even if the request itself was cancelled.
		if _, undoErr := h.db.Trials.UpdateOne(context.WithoutCancel(r.Context()),
			bson.M{"_id": id, "clientId": c.ID},
			bson.M{"$set": bson.M{"clientId": nil, "status": t.Status, "flagReason": t.FlagReason}}); undoErr != nil {
			slog.Error("convert: could not release the trial", "trial", id.Hex(), "err", undoErr)
		}
		writeServerError(w, r, err)
		return
	}

	t.ClientID, t.Status, t.FlagReason = &c.ID, "converted", ""
	writeJSON(w, http.StatusCreated, map[string]any{"client": c, "trial": t})
}

// POST /api/admin/trials/{id}/email
func (h *Handler) EmailTrial(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var t models.Trial
	if !h.load(w, r, h.db.Trials, id, &t) {
		return
	}
	var in emailInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if f := in.validate(); len(f) > 0 {
		writeInvalid(w, f)
		return
	}
	if !h.sendReply(w, r, t.Email, in) {
		return
	}

	now := models.Now()
	t.LastEmailedAt, t.Read = &now, true
	if h.save(w, r, h.db.Trials, id, t) {
		writeJSON(w, http.StatusOK, t)
	}
}
