package handlers

import (
	"log/slog"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"

	"dado/internal/db"
	"dado/internal/models"
)

var contactStatuses = []string{"new", "replied", "archived", "flagged"}

// contactInput is the public contact form.
type contactInput struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Service string `json:"service"`
	Message string `json:"message"`
	Website string `json:"website"` // honeypot: hidden from people, filled in by bots
}

func (in *contactInput) validate() fieldErrors {
	f := fieldErrors{}
	f.text("name", "Your name", &in.Name, true, 100)
	f.email("email", "Email address", &in.Email)
	f.phone("phone", &in.Phone)
	f.text("service", "Service", &in.Service, false, 120)
	f.detail("message", "Message", &in.Message)
	return f
}

// POST /api/contacts
func (h *Handler) CreateContact(w http.ResponseWriter, r *http.Request) {
	var in contactInput
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

	c := models.Contact{
		ID: bson.NewObjectID(), Name: in.Name, Email: in.Email, Phone: in.Phone,
		Service: in.Service, Message: in.Message, Status: "new", CreatedAt: models.Now(),
	}
	if _, err := h.db.Contacts.InsertOne(r.Context(), c); err != nil {
		writeServerError(w, r, err)
		return
	}

	// The message is saved; the emails go out in the background and a mail
	// problem can't lose it.
	if alert, receipt, err := h.mail.ContactEmails(c); err != nil {
		slog.Error("contact emails not built", "err", err)
	} else {
		h.mail.Go(alert, receipt)
	}
	writeJSON(w, http.StatusCreated, okBody)
}

// GET /api/admin/contacts
func (h *Handler) ListContacts(w http.ResponseWriter, r *http.Request) {
	list, err := db.FindAll[models.Contact](r.Context(), h.db.Contacts, bson.M{}, bson.D{{Key: "createdAt", Value: -1}})
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// PATCH /api/admin/contacts/{id}
func (h *Handler) PatchContact(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var c models.Contact
	if !h.load(w, r, h.db.Contacts, id, &c) {
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
	if f := p.apply(&c.Status, &c.FlagReason, &c.Read, contactStatuses); len(f) > 0 {
		writeInvalid(w, f)
		return
	}
	if h.save(w, r, h.db.Contacts, id, c) {
		writeJSON(w, http.StatusOK, c)
	}
}

// POST /api/admin/contacts/{id}/email
func (h *Handler) EmailContact(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var c models.Contact
	if !h.load(w, r, h.db.Contacts, id, &c) {
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
	if !h.sendReply(w, r, c.Email, in) {
		return
	}

	now := models.Now()
	c.LastEmailedAt, c.Read = &now, true
	if c.Status == "new" {
		c.Status = "replied"
	}
	if h.save(w, r, h.db.Contacts, id, c) {
		writeJSON(w, http.StatusOK, c)
	}
}
