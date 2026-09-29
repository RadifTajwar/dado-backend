// Package config reads settings from the environment, with a .env file as a
// convenience for local development. Real environment variables always win.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port         string
	SiteURL      string // public URL of the Next.js site, used in email links
	CookieSecure bool

	MongoURI string
	MongoDB  string

	JWTSecret     []byte
	AdminEmail    string // first admin, created when none exists
	AdminPassword string

	// Email goes out through the Gmail API over HTTPS (Render's free plan
	// blocks SMTP ports). Empty = email disabled; everything else still works.
	GmailSender       string // the Gmail account that sends (the From address)
	GmailClientID     string
	GmailClientSecret string
	GmailRefreshToken string
	MailFromName      string
	NotifyEmail       string // where new contact / trial alerts go

	CloudinaryCloud  string // empty = uploads disabled (503)
	CloudinaryKey    string
	CloudinarySecret string
}

// Load reads envFile (if it exists) and then the environment.
// It fails fast when a required setting is missing.
func Load(envFile string) (*Config, error) {
	if err := loadDotEnv(envFile); err != nil {
		return nil, err
	}

	c := &Config{
		Port:         get("PORT", "8080"),
		SiteURL:      strings.TrimRight(get("SITE_URL", "http://localhost:3000"), "/"),
		CookieSecure: get("COOKIE_SECURE", "false") == "true",

		MongoURI: get("MONGODB_URI", ""),
		MongoDB:  get("MONGODB_DB", "dado"),

		JWTSecret:     []byte(get("JWT_SECRET", "")),
		AdminEmail:    strings.ToLower(get("ADMIN_EMAIL", "")),
		AdminPassword: get("ADMIN_PASSWORD", ""),

		GmailSender:       strings.ToLower(get("GMAIL_SENDER", "")),
		GmailClientID:     get("GMAIL_CLIENT_ID", ""),
		GmailClientSecret: get("GMAIL_CLIENT_SECRET", ""),
		GmailRefreshToken: get("GMAIL_REFRESH_TOKEN", ""),
		MailFromName:      get("MAIL_FROM_NAME", "DADO Post-Production"),
		NotifyEmail:       get("NOTIFY_EMAIL", ""),

		CloudinaryCloud:  get("CLOUDINARY_CLOUD_NAME", ""),
		CloudinaryKey:    get("CLOUDINARY_API_KEY", ""),
		CloudinarySecret: get("CLOUDINARY_API_SECRET", ""),
	}
	if c.NotifyEmail == "" {
		c.NotifyEmail = c.GmailSender
	}

	var missing []string
	for key, val := range map[string]string{
		"MONGODB_URI": c.MongoURI,
		"JWT_SECRET":  string(c.JWTSecret),
	} {
		if val == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required settings: %s (see .env.example)", strings.Join(missing, ", "))
	}
	if len(c.JWTSecret) < 32 {
		return nil, errors.New("JWT_SECRET must be at least 32 characters")
	}
	return c, nil
}

// MailEnabled reports whether all four Gmail settings are set.
func (c *Config) MailEnabled() bool {
	return c.GmailSender != "" && c.GmailClientID != "" && c.GmailClientSecret != "" && c.GmailRefreshToken != ""
}

// UploadsEnabled reports whether all three Cloudinary keys are set.
func (c *Config) UploadsEnabled() bool {
	return c.CloudinaryCloud != "" && c.CloudinaryKey != "" && c.CloudinarySecret != ""
}

func get(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv sets KEY=VALUE lines from path without overriding variables that
// are already set. A missing file is fine (production uses real env vars).
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
	return sc.Err()
}
