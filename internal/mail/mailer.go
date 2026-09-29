// Package mail sends the site's emails through the Gmail API (HTTPS).
package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"

	"dado/internal/config"
)

// ErrNotConfigured means the GMAIL_* settings are empty, so nothing is sent.
var ErrNotConfigured = errors.New("email is not set up (the GMAIL_* settings are empty)")

// Google's endpoints. Tests point them at a local server.
var (
	tokenURL = "https://oauth2.googleapis.com/token"
	sendURL  = "https://gmail.googleapis.com/gmail/v1/users/me/messages/send"
)

// Message is one email with an HTML body and a plain-text alternative.
type Message struct {
	To      string
	ReplyTo string // optional
	Subject string
	HTML    string
	Text    string
}

type Mailer struct {
	enabled bool
	from    mail.Address
	oauth   url.Values // what Google needs to hand out an access token
	client  *http.Client

	mu      sync.Mutex // guards the cached access token
	token   string
	expires time.Time

	siteURL     string
	notifyEmail string

	pending sync.WaitGroup // background sends, so shutdown can wait for them
}

func New(cfg *config.Config) *Mailer {
	return &Mailer{
		enabled: cfg.MailEnabled(),
		from:    mail.Address{Name: cleanHeader(cfg.MailFromName), Address: cfg.GmailSender},
		oauth: url.Values{
			"client_id":     {cfg.GmailClientID},
			"client_secret": {cfg.GmailClientSecret},
			"refresh_token": {cfg.GmailRefreshToken},
			"grant_type":    {"refresh_token"},
		},
		client:      &http.Client{Timeout: 30 * time.Second}, // a slow Google answer can't hang a request
		siteURL:     cfg.SiteURL,
		notifyEmail: cfg.NotifyEmail,
	}
}

// Send delivers one message now through the Gmail API. It goes over HTTPS
// (port 443) because hosts like Render's free plan block the SMTP ports.
func (m *Mailer) Send(msg Message) error {
	if !m.enabled {
		return ErrNotConfigured
	}
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	raw, err := m.build(msg, to)
	if err != nil {
		return err
	}
	token, err := m.accessToken()
	if err != nil {
		return err
	}

	body, _ := json.Marshal(map[string]string{"raw": base64.URLEncoding.EncodeToString(raw)})
	req, err := http.NewRequest(http.MethodPost, sendURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if err := m.do(req, nil); err != nil {
		m.forgetToken() // if Google rejected the token, the next send fetches a fresh one
		return fmt.Errorf("gmail send: %w", err)
	}
	return nil
}

// accessToken trades the long-lived refresh token for an access token and
// reuses it until a minute before it expires (they last about an hour).
func (m *Mailer) accessToken() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.token != "" && time.Now().Before(m.expires) {
		return m.token, nil
	}

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(m.oauth.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := m.do(req, &out); err != nil {
		return "", fmt.Errorf("gmail token: %w", err)
	}
	if out.AccessToken == "" {
		return "", errors.New("gmail token: Google returned no access token")
	}
	m.token = out.AccessToken
	m.expires = time.Now().Add(time.Duration(out.ExpiresIn)*time.Second - time.Minute)
	return m.token, nil
}

func (m *Mailer) forgetToken() {
	m.mu.Lock()
	m.token = ""
	m.mu.Unlock()
}

// do sends req and decodes a JSON answer into out (when out isn't nil). Any
// non-2xx answer becomes an error that carries Google's explanation.
func (m *Mailer) do(req *http.Request, out any) error {
	res, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("%s: %s", res.Status, bytes.TrimSpace(detail))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// Go sends messages in the background, in order. Failures are logged, never
// returned: callers use this only after their data is already saved.
func (m *Mailer) Go(msgs ...Message) {
	m.Later(func() {
		for _, msg := range msgs {
			if err := m.Send(msg); err != nil {
				slog.Error("email not sent", "subject", msg.Subject, "err", err)
			}
		}
	})
}

// Later runs fn in the background, counted by Wait, for work that ends in an
// email. The count starts here, before the request returns, so shutdown
// can't miss it.
func (m *Mailer) Later(fn func()) {
	m.pending.Add(1)
	go func() {
		defer m.pending.Done()
		fn()
	}()
}

// Wait blocks until background work has finished (used on shutdown).
func (m *Mailer) Wait() { m.pending.Wait() }

// build writes the MIME message: headers, then text and HTML alternatives.
func (m *Mailer) build(msg Message, to *mail.Address) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, part := range []struct{ kind, content string }{
		{"text/plain", msg.Text}, // first = least preferred
		{"text/html", msg.HTML},
	} {
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.kind + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(pw)
		if _, err := qp.Write([]byte(part.content)); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	var head bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&head, "%s: %s\r\n", k, v) }
	header("From", m.from.String())
	header("To", to.String())
	if msg.ReplyTo != "" {
		if rt, err := mail.ParseAddress(msg.ReplyTo); err == nil {
			header("Reply-To", rt.String())
		}
	}
	header("Subject", mime.QEncoding.Encode("utf-8", cleanHeader(msg.Subject)))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("Message-ID", messageID(m.from.Address))
	header("MIME-Version", "1.0")
	header("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	head.WriteString("\r\n")

	return append(head.Bytes(), body.Bytes()...), nil
}

// ValidAddress reports whether s is a plain address (no name, no comments)
// that the mailer can send to. The regex in handlers lets through a few forms,
// like "a..b@x.com", that net/mail refuses.
func ValidAddress(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}

// cleanHeader removes line breaks, which would otherwise let text from a form
// inject extra email headers.
func cleanHeader(s string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(s))
}

func messageID(from string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	_, domain, _ := strings.Cut(from, "@")
	return "<" + hex.EncodeToString(b) + "@" + domain + ">"
}
