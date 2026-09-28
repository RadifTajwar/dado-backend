package mail

import (
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dado/internal/models"
)

func testMailer() *Mailer {
	return &Mailer{
		from:        mail.Address{Name: "DADO Post-Production", Address: "studio@example.com"},
		siteURL:     "http://localhost:3000",
		notifyEmail: "studio@example.com",
	}
}

// every email the site can send, built from one contact and one trial
func allMessages(t *testing.T, c models.Contact, tr models.Trial) map[string]Message {
	t.Helper()
	m := testMailer()
	out := map[string]Message{}
	var err error
	if out["contact_alert"], out["contact_receipt"], err = m.ContactEmails(c); err != nil {
		t.Fatal(err)
	}
	if out["trial_alert"], out["trial_receipt"], err = m.TrialEmails(tr); err != nil {
		t.Fatal(err)
	}
	if out["reply"], err = m.Reply(c.Email, "Re: Ghost mannequin — DADO", "Hi "+c.Name+",\n\nThanks for getting in touch.\nShort answer: yes.\n\nBest,\nDADO"); err != nil {
		t.Fatal(err)
	}
	if out["reset"], err = m.PasswordReset("admin@example.com", "http://localhost:3000/admin/reset-password?token=abc123"); err != nil {
		t.Fatal(err)
	}
	return out
}

func realistic() (models.Contact, models.Trial) {
	at := time.Date(2026, 9, 28, 8, 14, 0, 0, time.UTC)
	return models.Contact{
			Name: "Hannah Weiss", Email: "hannah@studioweiss.de", Phone: "+49 30 1234 5678",
			Service: "Ghost mannequin & liquify", CreatedAt: at,
			Message: "We shoot roughly 400 apparel SKUs a month.\nCan you hold a fixed look across a season?",
		}, models.Trial{
			Name: "Grace Mbeki", Email: "grace@sundaceramics.com", Company: "Sunda Ceramics",
			Services: []string{"photo", "video"}, Link: "https://drive.google.com/drive/folders/1aB9xKp",
			Volume: "100–500", Deadline: "Standard — 24h photo / voice, 48h video", NDA: true, CreatedAt: at,
			Brief: "Cut out on pure white, natural shadow, colour matched to the swatch in the folder.\nExport 2000px square JPG named by SKU.",
		}
}

func TestEveryEmailRendersAndEscapesInput(t *testing.T) {
	c, tr := realistic()
	evil := `<script>alert(1)</script>`
	c.Name, c.Message, c.Service = "Eve "+evil, "hi "+evil, evil
	tr.Name, tr.Company, tr.Brief = "Eve "+evil, evil, evil

	for kind, msg := range allMessages(t, c, tr) {
		if msg.HTML == "" || msg.Text == "" {
			t.Errorf("%s: empty body", kind)
		}
		if strings.Contains(msg.HTML, "<script>") {
			t.Errorf("%s: user input was not escaped", kind)
		}
		if !strings.Contains(msg.HTML, "hello@dado.studio") {
			t.Errorf("%s: missing the shared footer", kind)
		}
	}
}

func TestHeadersCannotBeInjected(t *testing.T) {
	m := testMailer()
	to, _ := mail.ParseAddress("someone@example.com")
	raw, err := m.build(Message{To: to.Address, Subject: "Hello\r\nBcc: attacker@example.com", HTML: "<p>x</p>", Text: "x"}, to)
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(raw), "\r\n\r\n")
	if strings.Contains(head, "\r\nBcc:") {
		t.Fatalf("subject broke out into a new header:\n%s", head)
	}
}

// Preview writes every email as an .html file so it can be looked at in a
// browser or with Quick Look:
//
//	MAIL_PREVIEW_DIR=/tmp/dado-mail go test ./internal/mail -run Preview
func TestPreview(t *testing.T) {
	dir := os.Getenv("MAIL_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set MAIL_PREVIEW_DIR to write previews")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	c, tr := realistic()
	for kind, msg := range allMessages(t, c, tr) {
		if err := os.WriteFile(filepath.Join(dir, kind+".html"), []byte(msg.HTML), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, kind+".txt"), []byte(msg.Text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A visitor's confirmation must never repeat what was typed into the form,
// or the forms could make the studio email anyone a spammer's text.
func TestReceiptsDoNotEchoVisitorText(t *testing.T) {
	c, tr := realistic()
	c.Name, c.Phone, c.Service, c.Message = "Zqx Name", "+1 555 0100 zqx", "Zqx service", "Zqx buy cheap pills"
	tr.Name, tr.Company, tr.Phone, tr.Brief = "Zqx Name", "Zqx Ltd", "+1 555 0100 zqx", "Zqx brief text"
	tr.Link, tr.Volume, tr.Deadline = "https://drive.google.com/drive/folders/zqx", "zqx volume", "zqx deadline"

	msgs := allMessages(t, c, tr)
	for _, kind := range []string{"contact_receipt", "trial_receipt"} {
		m := msgs[kind]
		if all := strings.ToLower(m.Subject + m.HTML + m.Text); strings.Contains(all, "zqx") {
			t.Errorf("%s repeats visitor text", kind)
		}
	}
	if r := msgs["trial_receipt"]; !strings.Contains(r.HTML, "Photo, Video") || !strings.Contains(r.Text, "Requested") {
		t.Error("trial receipt should list the chosen services and the NDA choice")
	}
	// the studio's own alerts keep every detail
	if !strings.Contains(msgs["contact_alert"].HTML, "Zqx buy cheap pills") || !strings.Contains(msgs["trial_alert"].HTML, "Zqx brief text") {
		t.Error("alerts lost the visitor's details")
	}
}
