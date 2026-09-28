// Package mail sends the site's emails through Gmail SMTP.
package mail

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"dado/internal/config"
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
	host, port string
	user, pass string
	from       mail.Address

	siteURL     string
	notifyEmail string

	pending sync.WaitGroup // background sends, so shutdown can wait for them
}

func New(cfg *config.Config) *Mailer {
	return &Mailer{
		host:        cfg.SMTPHost,
		port:        cfg.SMTPPort,
		user:        cfg.SMTPUser,
		pass:        cfg.SMTPPass,
		from:        mail.Address{Name: cleanHeader(cfg.MailFromName), Address: cfg.SMTPUser},
		siteURL:     cfg.SiteURL,
		notifyEmail: cfg.NotifyEmail,
	}
}

// Send delivers one message now. Every network step has a deadline, so a
// stuck SMTP server can't hang a request.
func (m *Mailer) Send(msg Message) error {
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	raw, err := m.build(msg, to)
	if err != nil {
		return err
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(m.host, m.port), 10*time.Second)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, m.host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()

	if err := c.StartTLS(&tls.Config{ServerName: m.host}); err != nil {
		return err
	}
	if err := c.Auth(smtp.PlainAuth("", m.user, m.pass, m.host)); err != nil {
		return err
	}
	if err := c.Mail(m.from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(to.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
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
