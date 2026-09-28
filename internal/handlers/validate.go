package handlers

import (
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"dado/internal/mail"
)

/* Validation rules. The wording matches frontend/lib/validation.ts so a
   message reads the same whether the browser or the API catches it. The
   browser checks are only a convenience; these are the ones that count. */

var (
	// The domain part is letters, digits, dots and hyphens only, so an address
	// can't smuggle "?bcc=…" into a mailto: link or a mail header.
	emailRe = regexp.MustCompile(`(?i)^[^\s@]+@[a-z0-9-]+(\.[a-z0-9-]+)*\.[a-z]{2,}$`)

	// Cloud hosts accepted for trial files, matched against the whole host
	// name (or a subdomain of it), so "dropbox.mail-verify.example" fails.
	// Same list as CLOUD_HOSTS in frontend/lib/validation.ts.
	cloudHosts = regexp.MustCompile(`(?i)(^|\.)(drive\.google\.com|docs\.google\.com|dropbox\.com|dropboxusercontent\.com|wetransfer\.com|we\.tl|onedrive\.live\.com|1drv\.ms|sharepoint\.com|icloud\.com|box\.com|mega\.nz|mega\.io|amazonaws\.com|frame\.io|f\.io|pcloud\.com|pcloud\.link|sync\.com|terabox\.com|mediafire\.com)$`)

	disciplines = []string{"photo", "video", "voice"}
)

// fieldErrors maps an input name to the first problem found with it.
type fieldErrors map[string]string

func (f fieldErrors) add(field, message string) {
	if _, taken := f[field]; !taken {
		f[field] = message
	}
}

func (f fieldErrors) has(field string) bool {
	_, ok := f[field]
	return ok
}

// text trims *v, then checks it is present (when required) and short enough.
func (f fieldErrors) text(field, label string, v *string, required bool, max int) {
	*v = strings.TrimSpace(*v)
	switch {
	case required && *v == "":
		f.add(field, label+" is required.")
	case utf8.RuneCountInString(*v) > max:
		f.add(field, label+" is too long.")
	}
}

func (f fieldErrors) email(field, label string, v *string) {
	f.text(field, label, v, true, 254)
	if !f.has(field) && (!emailRe.MatchString(*v) || !mail.ValidAddress(*v)) {
		f.add(field, "Enter a valid email address.")
	}
}

// phone is optional, but when given it needs at least 7 digits.
func (f fieldErrors) phone(field string, v *string) {
	*v = strings.TrimSpace(*v)
	digits := 0
	for _, c := range *v {
		if unicode.IsDigit(c) {
			digits++
		}
	}
	switch {
	case utf8.RuneCountInString(*v) > 40:
		f.add(field, "That phone number is too long.")
	case *v != "" && digits < 7:
		f.add(field, "Enter a valid phone number.")
	}
}

// detail is a required free-text box that needs a real sentence (10+ chars).
func (f fieldErrors) detail(field, label string, v *string) {
	f.text(field, label, v, true, 5000)
	if !f.has(field) && utf8.RuneCountInString(*v) < 10 {
		f.add(field, "Please add a little more detail.")
	}
}

// cloudLink must be an http(s) link on a known file-sharing host.
func (f fieldErrors) cloudLink(field string, v *string) {
	f.text(field, "File link", v, true, 2000)
	if f.has(field) {
		return
	}
	u, err := url.Parse(*v)
	switch {
	case err != nil || u.Scheme == "":
		f.add(field, "Paste a full link starting with https://")
	case u.Scheme != "https" && u.Scheme != "http":
		f.add(field, "Link must start with http:// or https://")
	case u.Host == "":
		f.add(field, "Paste a full link starting with https://")
	case !cloudHosts.MatchString(u.Hostname()):
		f.add(field, "That does not look like a shared cloud link. Drive, Dropbox, WeTransfer, OneDrive and similar are accepted.")
	}
}

// optionalLink may be empty; otherwise it must be an http(s) URL.
func (f fieldErrors) optionalLink(field string, v *string) {
	f.text(field, "Link", v, false, 2000)
	if f.has(field) || *v == "" {
		return
	}
	u, err := url.Parse(*v)
	switch {
	case err != nil || u.Host == "":
		f.add(field, "That is not a valid link.")
	case u.Scheme != "https" && u.Scheme != "http":
		f.add(field, "Link must start with http:// or https://")
	}
}

// imageSources are the only places next/image may load images from. This list
// mirrors images.remotePatterns in frontend/next.config.ts and
// frontend/lib/admin/images.ts. An image from anywhere else would crash the
// public page that shows it, so it is refused here.
// "dzmhtdw6b" is CLOUDINARY_CLOUD_NAME: if the account changes, update all three.
var imageSources = []struct{ host, pathPrefix string }{
	{"res.cloudinary.com", "/dzmhtdw6b/"}, // our Cloudinary account only
	{"images.unsplash.com", "/"},          // starter images
}

// imageURL is required, https, and from one of imageSources.
func (f fieldErrors) imageURL(field, label string, v *string) {
	f.text(field, label, v, true, 2000)
	if f.has(field) {
		return
	}
	if u, err := url.Parse(*v); err != nil || !allowedImage(u) {
		f.add(field, "Use an https image from Cloudinary or Unsplash.")
	}
}

func allowedImage(u *url.URL) bool {
	if u.Scheme != "https" || strings.Contains(u.Path, "..") {
		return false
	}
	return slices.ContainsFunc(imageSources, func(s struct{ host, pathPrefix string }) bool {
		return u.Host == s.host && strings.HasPrefix(u.Path, s.pathPrefix)
	})
}

func (f fieldErrors) oneOf(field, value, message string, allowed ...string) {
	if !slices.Contains(allowed, value) {
		f.add(field, message)
	}
}

// date must be a real calendar day written as YYYY-MM-DD.
func (f fieldErrors) date(field string, v *string) {
	*v = strings.TrimSpace(*v)
	if _, err := time.Parse(time.DateOnly, *v); err != nil {
		f.add(field, "Enter a date as YYYY-MM-DD.")
	}
}

// money rounds to cents and checks the range.
func (f fieldErrors) money(field, message string, v *float64) {
	*v = math.Round(*v*100) / 100
	if *v < 0 || *v > 1_000_000 || math.IsNaN(*v) {
		f.add(field, message)
	}
}
