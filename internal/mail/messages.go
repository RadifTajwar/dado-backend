package mail

import (
	"bytes"
	"embed"
	htmltpl "html/template"
	"net/url"
	"strings"
	texttpl "text/template"
	"unicode/utf8"

	"dado/internal/models"
)

//go:embed templates
var files embed.FS

// The kinds of email, each with an .html (inside layout.html) and a .txt template.
const (
	kindContactAlert   = "contact_alert"
	kindContactReceipt = "contact_receipt"
	kindTrialAlert     = "trial_alert"
	kindTrialReceipt   = "trial_receipt"
	kindReply          = "reply"
	kindReset          = "reset"
)

var funcs = map[string]any{
	// lines splits text on line breaks so templates can join the pieces with
	// <br>; each piece is still escaped by html/template.
	"lines": func(s string) []string {
		return strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	},
}

var (
	htmlTemplates = map[string]*htmltpl.Template{}
	textTemplates = map[string]*texttpl.Template{}
)

func init() {
	for _, kind := range []string{kindContactAlert, kindContactReceipt, kindTrialAlert, kindTrialReceipt, kindReply, kindReset} {
		htmlTemplates[kind] = htmltpl.Must(htmltpl.New("layout.html").Funcs(funcs).
			ParseFS(files, "templates/layout.html", "templates/"+kind+".html"))
		textTemplates[kind] = texttpl.Must(texttpl.New(kind+".txt").Funcs(funcs).
			ParseFS(files, "templates/layout.txt", "templates/"+kind+".txt"))
	}
}

// frame is what the shared layout needs; each email embeds it in its own data.
type frame struct {
	Subject   string
	Preheader string // the grey preview line in the inbox list
	SiteURL   string
	Reason    string // small print: why the reader got this email
}

func render(kind string, data any) (html, text string, err error) {
	var h, t bytes.Buffer
	if err := htmlTemplates[kind].ExecuteTemplate(&h, "layout.html", data); err != nil {
		return "", "", err
	}
	if err := textTemplates[kind].ExecuteTemplate(&t, kind+".txt", data); err != nil {
		return "", "", err
	}
	return h.String(), t.String(), nil
}

// receiptData is everything a visitor's own confirmation email may show:
// fixed text plus values from fixed lists. Nothing typed into a form goes in,
// so the forms can't be used to make the studio email someone a stranger's words.
type receiptData struct {
	frame
	Services string // "Photo, Video": fixed labels for the chosen disciplines
	NDA      bool
}

/* ---------- contact form ---------- */

type contactData struct {
	frame
	models.Contact
	FirstName string
	Received  string
	AdminURL  string
	ReplyURL  string // mailto: with a prefilled subject
}

// ContactEmails builds the studio alert and the thank-you reply for a new message.
func (m *Mailer) ContactEmails(c models.Contact) (alert, receipt Message, err error) {
	d := contactData{
		Contact:   c,
		FirstName: firstName(c.Name),
		Received:  c.CreatedAt.In(models.Dhaka).Format("Mon 2 Jan 2006, 15:04") + " Dhaka time",
		AdminURL:  m.siteURL + "/admin/contacts",
		ReplyURL:  "mailto:" + c.Email + "?subject=" + url.PathEscape("Re: your message to DADO"),
	}

	d.frame = frame{
		Subject:   "New message from " + c.Name + suffix(" · ", c.Service),
		Preheader: excerpt(c.Message, 110),
		SiteURL:   m.siteURL,
		Reason:    "Sent to the studio inbox because someone used the contact form on the DADO website.",
	}
	if alert, err = m.message(kindContactAlert, m.notifyEmail, c.Email, d); err != nil {
		return
	}

	receipt, err = m.message(kindContactReceipt, c.Email, m.notifyEmail, receiptData{frame: frame{
		Subject:   "Thanks for contacting DADO — we'll be in touch soon",
		Preheader: "A producer will reply within 30 minutes, day or night.",
		SiteURL:   m.siteURL,
		Reason:    "You're getting this because this address was used on the DADO contact form. Just reply if you need anything.",
	}})
	return
}

/* ---------- free-trial form ---------- */

type trialData struct {
	frame
	models.Trial
	FirstName    string
	Who          string // company, or the person's name
	ServiceNames string // "Photo, Video"
	LinkHost     string
	Received     string
	AdminURL     string
	ReplyURL     string
}

// TrialEmails builds the studio alert and the confirmation for a new trial request.
func (m *Mailer) TrialEmails(t models.Trial) (alert, receipt Message, err error) {
	d := trialData{
		Trial:        t,
		FirstName:    firstName(t.Name),
		Who:          firstNonEmpty(t.Company, t.Name),
		ServiceNames: serviceNames(t.Services),
		LinkHost:     linkHost(t.Link),
		Received:     t.CreatedAt.In(models.Dhaka).Format("Mon 2 Jan 2006, 15:04") + " Dhaka time",
		AdminURL:     m.siteURL + "/admin/trials",
		ReplyURL:     "mailto:" + t.Email + "?subject=" + url.PathEscape("Your DADO trial"),
	}

	d.frame = frame{
		Subject:   "New trial request: " + d.Who + " (" + d.ServiceNames + ")",
		Preheader: excerpt(t.Brief, 110),
		SiteURL:   m.siteURL,
		Reason:    "Sent to the studio inbox because someone asked for a free trial on the DADO website.",
	}
	if alert, err = m.message(kindTrialAlert, m.notifyEmail, t.Email, d); err != nil {
		return
	}

	receipt, err = m.message(kindTrialReceipt, t.Email, m.notifyEmail, receiptData{
		frame: frame{
			Subject:   "Your DADO trial request is in",
			Preheader: "We're opening your link now. Expect a reply within 30 minutes.",
			SiteURL:   m.siteURL,
			Reason:    "You're getting this because this address was used on the DADO free-trial form. Just reply if you need anything.",
		},
		Services: d.ServiceNames,
		NDA:      t.NDA,
	})
	return
}

/* ---------- admin reply ---------- */

type replyData struct {
	frame
	Paragraphs [][]string // blank line = new paragraph, single line break = <br>
}

// Reply wraps an admin's plain-text email in the branded frame.
func (m *Mailer) Reply(to, subject, body string) (Message, error) {
	var paras [][]string
	for _, p := range strings.Split(strings.ReplaceAll(body, "\r", ""), "\n\n") {
		if p = strings.Trim(p, "\n"); strings.TrimSpace(p) != "" {
			paras = append(paras, strings.Split(p, "\n"))
		}
	}
	d := replyData{
		frame: frame{
			Subject:   subject,
			Preheader: excerpt(body, 110),
			SiteURL:   m.siteURL,
			Reason:    "You're receiving this because you were in touch with DADO Post-Production. Reply to this email to answer.",
		},
		Paragraphs: paras,
	}
	return m.message(kindReply, to, m.notifyEmail, d)
}

/* ---------- password reset ---------- */

type resetData struct {
	frame
	Email string
	Link  string
}

func (m *Mailer) PasswordReset(to, link string) (Message, error) {
	d := resetData{
		frame: frame{
			Subject:   "Reset your DADO admin password",
			Preheader: "This link works once and expires in 30 minutes.",
			SiteURL:   m.siteURL,
			Reason:    "Sent because a password reset was requested for the DADO admin account.",
		},
		Email: to,
		Link:  link,
	}
	return m.message(kindReset, to, "", d)
}

/* ---------- helpers ---------- */

func (m *Mailer) message(kind, to, replyTo string, data interface{ subject() string }) (Message, error) {
	html, text, err := render(kind, data)
	if err != nil {
		return Message{}, err
	}
	return Message{To: to, ReplyTo: replyTo, Subject: data.subject(), HTML: html, Text: text}, nil
}

func (f frame) subject() string { return f.Subject }

func firstName(name string) string {
	if f := strings.Fields(name); len(f) > 0 && f[0] != "—" {
		return f[0]
	}
	return "there"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func suffix(sep, s string) string {
	if s == "" {
		return ""
	}
	return sep + s
}

// excerpt flattens text to one line and cuts it to n characters.
func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

var serviceLabels = map[string]string{"photo": "Photo", "video": "Video", "voice": "Voice"}

// serviceNames turns disciplines into fixed labels; anything else is dropped.
func serviceNames(services []string) string {
	var names []string
	for _, s := range services {
		if label, ok := serviceLabels[s]; ok {
			names = append(names, label)
		}
	}
	return strings.Join(names, ", ")
}

func linkHost(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return link
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
