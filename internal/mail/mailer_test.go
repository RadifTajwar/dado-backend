package mail

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dado/internal/config"
)

// A fake Google: one access token for two sends, and the raw message arrives
// base64url-encoded with our headers.
func TestSendUsesGmailAPI(t *testing.T) {
	var tokenCalls, sendCalls int
	var raw string
	google := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls++
			if r.FormValue("refresh_token") != "refresh" || r.FormValue("grant_type") != "refresh_token" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			fmt.Fprint(w, `{"access_token":"access","expires_in":3599}`)
		case "/send":
			sendCalls++
			if r.Header.Get("Authorization") != "Bearer access" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var body struct{ Raw string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			b, err := base64.URLEncoding.DecodeString(body.Raw)
			if err != nil {
				http.Error(w, "bad raw", http.StatusBadRequest)
				return
			}
			raw = string(b)
			fmt.Fprint(w, `{"id":"1"}`)
		}
	}))
	defer google.Close()
	oldToken, oldSend := tokenURL, sendURL
	tokenURL, sendURL = google.URL+"/token", google.URL+"/send"
	t.Cleanup(func() { tokenURL, sendURL = oldToken, oldSend })

	m := New(&config.Config{
		GmailSender: "studio@example.com", GmailClientID: "id", GmailClientSecret: "secret",
		GmailRefreshToken: "refresh", MailFromName: "DADO",
	})
	msg := Message{To: "client@example.com", ReplyTo: "admin@example.com", Subject: "Hello", HTML: "<p>Hi</p>", Text: "Hi"}
	for range 2 {
		if err := m.Send(msg); err != nil {
			t.Fatal(err)
		}
	}
	if tokenCalls != 1 || sendCalls != 2 {
		t.Errorf("token fetched %d times for %d sends; want 1 token for 2 sends", tokenCalls, sendCalls)
	}
	for _, want := range []string{"From: \"DADO\" <studio@example.com>", "To: <client@example.com>", "Reply-To: <admin@example.com>", "Subject: Hello"} {
		if !strings.Contains(raw, want) {
			t.Errorf("message is missing %q", want)
		}
	}
}

// Without the GMAIL_* settings nothing is sent and callers can tell why.
func TestSendWithoutSettings(t *testing.T) {
	if err := New(&config.Config{}).Send(Message{To: "a@example.com"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
}
