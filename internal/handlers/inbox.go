package handlers

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"dado/internal/mail"
)

/* Trials and contacts are both "inbox" items: the admin reads them, flags
   the bad ones and replies by email. The shared rules live here. */

// statusPatch is the body of PATCH /api/admin/{trials|contacts}/{id}.
type statusPatch struct {
	Status     *string `json:"status"`
	FlagReason *string `json:"flagReason"`
	Read       *bool   `json:"read"`
}

func (p statusPatch) empty() bool { return p.Status == nil && p.FlagReason == nil && p.Read == nil }

// apply changes status, reason and read on the record in place, following
// the rules: "flagged" needs a reason, other statuses clear it, "converted"
// only happens through Convert, and read is one-way.
func (p statusPatch) apply(status, reason *string, read *bool, allowed []string) fieldErrors {
	f := fieldErrors{}

	if p.Read != nil {
		if !*p.Read {
			f.add("read", "Marking as read can't be undone.")
		}
		*read = true
	}

	newReason := ""
	if p.FlagReason != nil {
		newReason = strings.TrimSpace(*p.FlagReason)
		if utf8.RuneCountInString(newReason) > 300 {
			f.add("flagReason", "Keep the reason under 300 characters.")
		}
	}

	switch {
	case p.Status == nil && p.FlagReason == nil:
		// only "read" changed
	case p.Status == nil: // editing the reason on a record that is already flagged
		if *status != "flagged" {
			f.add("flagReason", "Only a flagged record has a reason.")
		} else if newReason == "" {
			f.add("flagReason", "Give a reason so the next person knows.")
		}
		*reason = newReason
	case *p.Status == "converted":
		f.add("status", "Use Convert to turn a trial into a client.")
	case !slices.Contains(allowed, *p.Status):
		f.add("status", "Unknown status.")
	case *p.Status == "flagged":
		if newReason == "" {
			f.add("flagReason", "Give a reason so the next person knows.")
		}
		*status, *reason = "flagged", newReason
	default:
		*status, *reason = *p.Status, ""
	}
	return f
}

// emailInput is the body of POST /api/admin/{trials|contacts}/{id}/email.
type emailInput struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (in *emailInput) validate() fieldErrors {
	f := fieldErrors{}
	in.Subject = strings.TrimSpace(in.Subject)
	in.Body = strings.TrimSpace(in.Body)
	switch {
	case in.Subject == "":
		f.add("subject", "Give the email a subject.")
	case utf8.RuneCountInString(in.Subject) > 200:
		f.add("subject", "Subject is too long.")
	}
	switch {
	case utf8.RuneCountInString(in.Body) < 10:
		f.add("body", "Write a message first.")
	case utf8.RuneCountInString(in.Body) > 10000:
		f.add("body", "That message is too long.")
	}
	return f
}

// sendReply emails the admin's message right away. When SMTP fails it
// answers 502 and returns false, so the caller doesn't record a send that
// never happened.
func (h *Handler) sendReply(w http.ResponseWriter, r *http.Request, to string, in emailInput) bool {
	if !mail.ValidAddress(to) { // e.g. an old record saved before the stricter check
		writeError(w, http.StatusBadRequest, "This email address can't receive mail. Check it and update the record.")
		return false
	}
	msg, err := h.mail.Reply(to, in.Subject, in.Body)
	if err != nil {
		writeServerError(w, r, err)
		return false
	}
	if err := h.mail.Send(msg); err != nil {
		slog.Error("admin email not sent", "err", err)
		writeError(w, http.StatusBadGateway, "The email could not be sent. Try again in a minute.")
		return false
	}
	return true
}
