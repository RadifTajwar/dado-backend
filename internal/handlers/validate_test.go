package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func validContact() contactInput {
	return contactInput{Name: "Jordan Ali", Email: "jordan@brand.com", Message: "Around 400 SKUs a month, can you help?"}
}

func validTrial() trialInput {
	return trialInput{
		Name: "Jordan Ali", Email: "jordan@brand.com", Services: []string{"video", "photo", "photo"},
		Link: "https://drive.google.com/drive/folders/abc", Brief: "Cut out on white with a soft shadow.",
	}
}

func TestContactRules(t *testing.T) {
	in := validContact()
	if f := in.validate(); len(f) > 0 {
		t.Fatalf("valid contact rejected: %v", f)
	}

	cases := map[string]struct {
		edit  func(*contactInput)
		field string
		want  string
	}{
		"name missing":                 {func(c *contactInput) { c.Name = "   " }, "name", "Your name is required."},
		"email malformed":              {func(c *contactInput) { c.Email = "a@b" }, "email", "Enter a valid email address."},
		"email net/mail refuses":       {func(c *contactInput) { c.Email = "a..b@x.com" }, "email", "Enter a valid email address."},
		"email with comment":           {func(c *contactInput) { c.Email = "a(b)@x.com" }, "email", "Enter a valid email address."},
		"email smuggles mailto params": {func(c *contactInput) { c.Email = "jo@brand.com?bcc=spy%40evil.com" }, "email", "Enter a valid email address."},
		"phone too short":              {func(c *contactInput) { c.Phone = "12-34" }, "phone", "Enter a valid phone number."},
		"message short":                {func(c *contactInput) { c.Message = "hi" }, "message", "Please add a little more detail."},
		"message missing":              {func(c *contactInput) { c.Message = "" }, "message", "Message is required."},
		"name too long":                {func(c *contactInput) { c.Name = strings.Repeat("a", 101) }, "name", "Your name is too long."},
	}
	for name, tc := range cases {
		in := validContact()
		tc.edit(&in)
		if got := in.validate()[tc.field]; got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestTrialRules(t *testing.T) {
	in := validTrial()
	if f := in.validate(); len(f) > 0 {
		t.Fatalf("valid trial rejected: %v", f)
	}
	if got := strings.Join(in.Services, ","); got != "photo,video" {
		t.Errorf("services not cleaned up: %q", got)
	}

	links := map[string]string{
		"":                                      "File link is required.",
		"drive.google.com/x":                    "Paste a full link starting with https://",
		"ftp://drive.google.com/x":              "Link must start with http:// or https://",
		"javascript:alert(1)":                   "Link must start with http:// or https://",
		"https://example.com/files":             "That does not look like a shared cloud link. Drive, Dropbox, WeTransfer, OneDrive and similar are accepted.",
		"https://www.dropbox.com/scl/fo":        "",
		"https://www.dropbox.com/scl/fo/x":      "",
		"https://app.box.com/s/x":               "",
		"https://dropbox.mail-verify.example/x": "That does not look like a shared cloud link. Drive, Dropbox, WeTransfer, OneDrive and similar are accepted.",
		"https://evil-dropbox.com/x":            "That does not look like a shared cloud link. Drive, Dropbox, WeTransfer, OneDrive and similar are accepted.",
	}
	for link, want := range links {
		in := validTrial()
		in.Link = link
		if got := in.validate()["link"]; got != want {
			t.Errorf("link %q: got %q, want %q", link, got, want)
		}
	}

	in = validTrial()
	in.Services = nil
	if got := in.validate()["services"]; got != "Pick at least one service." {
		t.Errorf("empty services: got %q", got)
	}
	in = validTrial()
	in.Services = []string{"photo", "drone"}
	if got := in.validate()["services"]; got != "Choose photo, video or voice." {
		t.Errorf("unknown service: got %q", got)
	}
}

// A filled honeypot gets a normal-looking 201 and never touches the database
// (the Handler here has no database at all).
func TestHoneypotIsAcceptedAndDropped(t *testing.T) {
	h := &Handler{}
	for path, fn := range map[string]http.HandlerFunc{"/api/contacts": h.CreateContact, "/api/trials": h.CreateTrial} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"website":"http://spam.example","name":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		fn(rec, req)
		if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"ok":true`) {
			t.Errorf("%s: got %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestStatusPatchRules(t *testing.T) {
	str := func(s string) *string { return &s }
	yes, no := true, false

	status, reason, read := "new", "", false
	if f := (statusPatch{Status: str("flagged")}).apply(&status, &reason, &read, contactStatuses); f["flagReason"] == "" {
		t.Error("flagging without a reason should fail")
	}
	status, reason = "new", ""
	if f := (statusPatch{Status: str("flagged"), FlagReason: str(" spam ")}).apply(&status, &reason, &read, contactStatuses); len(f) > 0 || status != "flagged" || reason != "spam" {
		t.Errorf("flag with reason: %v %q %q", f, status, reason)
	}
	if f := (statusPatch{Status: str("new")}).apply(&status, &reason, &read, contactStatuses); len(f) > 0 || reason != "" {
		t.Errorf("unflag should clear the reason: %v %q", f, reason)
	}
	if f := (statusPatch{Status: str("converted")}).apply(&status, &reason, &read, trialStatuses); f["status"] == "" {
		t.Error("converted must only come from Convert")
	}
	if f := (statusPatch{Read: &yes}).apply(&status, &reason, &read, contactStatuses); len(f) > 0 || !read {
		t.Error("marking read failed")
	}
	if f := (statusPatch{Read: &no}).apply(&status, &reason, &read, contactStatuses); f["read"] == "" {
		t.Error("read must be one-way")
	}
}

// Images must come from the hosts next/image allows (frontend/next.config.ts).
func TestImageURLHosts(t *testing.T) {
	const bad = "Use an https image from Cloudinary or Unsplash."
	cases := map[string]string{
		"https://res.cloudinary.com/dzmhtdw6b/image/upload/v1/dado/a.jpg": "",
		"https://res.cloudinary.com/demo/image/upload/v1/dado/a.jpg":      bad, // someone else's account
		"https://res.cloudinary.com/dzmhtdw6b/../demo/a.jpg":              bad,
		"https://images.unsplash.com/photo-1?auto=format&w=1600":          "",
		"":                                     "Image is required.",
		"http://res.cloudinary.com/demo/a.jpg": bad,
		"https://example.com/a.jpg":            bad,
		"https://res.cloudinary.com.evil.io/a.jp": bad,
		"https://res.cloudinary.com:8443/a.jpg":   bad,
		"javascript:alert(1)":                     bad,
	}
	for in, want := range cases {
		f := fieldErrors{}
		v := in
		f.imageURL("url", "Image", &v)
		if got := f["url"]; got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// An address the mailer can't use is the admin's to fix, not a server error.
func TestReplyToUnusableAddress(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/contacts/x/email", nil)
	if (&Handler{}).sendReply(rec, req, "a..b@x.com", emailInput{Subject: "Hi", Body: "A long enough body."}) {
		t.Fatal("sendReply accepted an address net/mail refuses")
	}
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "can't receive mail") {
		t.Errorf("got %d %s", rec.Code, rec.Body)
	}
}
