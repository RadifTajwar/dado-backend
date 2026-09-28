package auth

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var secret = []byte("test-secret-that-is-at-least-32-bytes-long")

func TestTokenRoundTrip(t *testing.T) {
	tok, err := IssueToken(secret, "abc123", "admin@example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseToken(secret, tok)
	if err != nil || c.Subject != "abc123" || c.Email != "admin@example.com" {
		t.Fatalf("round trip: %v %+v", err, c)
	}
	if got := c.ExpiresAt.Sub(c.IssuedAt.Time); got != 24*time.Hour {
		t.Errorf("token should last 24h, lasts %s", got)
	}
	if _, err := ParseToken([]byte("some-other-secret-that-is-32-bytes!!"), tok); err == nil {
		t.Error("token signed with another secret was accepted")
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	tok, _ := IssueToken(secret, "abc123", "admin@example.com", time.Now().Add(-25*time.Hour))
	if _, err := ParseToken(secret, tok); err == nil {
		t.Fatal("expired token was accepted")
	}
}

func TestOtherAlgorithmsRejected(t *testing.T) {
	claims := Claims{Email: "x@y.z", RegisteredClaims: jwt.RegisteredClaims{
		Subject: "abc", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	hs512, _ := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString(secret)
	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	for name, tok := range map[string]string{"HS512": hs512, "none": none} {
		if _, err := ParseToken(secret, tok); err == nil {
			t.Errorf("%s token was accepted", name)
		}
	}
}

func TestPasswords(t *testing.T) {
	long := strings.Repeat("x", 100) // past bcrypt's 72-byte limit
	for _, pw := range []string{"hunter22", long} {
		hash, err := HashPassword(pw)
		if err != nil {
			t.Fatal(err)
		}
		if !CheckPassword(hash, pw) || CheckPassword(hash, pw+"!") {
			t.Errorf("password check wrong for %d chars", len(pw))
		}
	}
	if CheckNothing("anything") {
		t.Error("CheckNothing must always fail")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	now := time.Now()
	for i := 1; i <= 3; i++ {
		if !l.Allow("1.2.3.4", now) {
			t.Fatalf("request %d blocked too early", i)
		}
	}
	if l.Allow("1.2.3.4", now) {
		t.Error("4th request in the window was allowed")
	}
	if !l.Allow("5.6.7.8", now) {
		t.Error("another IP was blocked")
	}
	if !l.Allow("1.2.3.4", now.Add(61*time.Second)) {
		t.Error("window did not reset")
	}
}

func TestClientIPIgnoresJunkForwardedFor(t *testing.T) {
	cases := map[string]string{
		"":                            "192.0.2.10", // no header: the connection's address
		"203.0.113.9":                 "203.0.113.9",
		" 203.0.113.9 , 10.0.0.1":     "203.0.113.9",
		"not-an-ip, 203.0.113.9":      "192.0.2.10", // junk first entry is ignored
		"203.0.113.9:1234":            "192.0.2.10", // host:port is not a bare IP
		"2001:db8::1":                 "2001:db8::1",
		strings.Repeat("9", 64) + ",": "192.0.2.10",
	}
	for header, want := range cases {
		r := httptest.NewRequest("POST", "/api/auth/login", nil)
		r.RemoteAddr = "192.0.2.10:5555"
		if header != "" {
			r.Header.Set("X-Forwarded-For", header)
		}
		if got := ClientIP(r); got != want {
			t.Errorf("XFF %q: got %q, want %q", header, got, want)
		}
	}
}

func TestIPv6CountsPerSlash64(t *testing.T) {
	a, b := limitKey("2001:db8:1:2:aaaa::1"), limitKey("2001:db8:1:2:bbbb::2")
	if a != b || a != "2001:db8:1:2::/64" {
		t.Errorf("same /64 should share a key: %q vs %q", a, b)
	}
	if limitKey("2001:db8:1:3::1") == a {
		t.Error("a different /64 should get its own key")
	}
	if got := limitKey("203.0.113.9"); got != "203.0.113.9" {
		t.Errorf("IPv4 should be keyed as-is, got %q", got)
	}

	l := NewLimiter(2, time.Minute)
	now := time.Now()
	l.Allow(limitKey("2001:db8:1:2::10"), now)
	l.Allow(limitKey("2001:db8:1:2::20"), now)
	if l.Allow(limitKey("2001:db8:1:2::30"), now) {
		t.Error("rotating addresses inside one /64 got past the limit")
	}
}

func TestLimiterMapIsCapped(t *testing.T) {
	l := NewLimiter(5, time.Hour)
	now := time.Now()
	for i := 0; i < maxKeys+10; i++ {
		l.Allow(fmt.Sprintf("key-%d", i), now) // all windows still live
		if len(l.hits) > maxKeys {
			t.Fatalf("map grew past the cap: %d keys", len(l.hits))
		}
	}
	if len(l.hits) > 10 {
		t.Errorf("a full map should start over, still has %d keys", len(l.hits))
	}

	// finished windows are dropped by the once-a-minute sweep
	l = NewLimiter(5, time.Minute)
	l.Allow("old", now)
	l.Allow("new", now.Add(2*time.Minute))
	if _, ok := l.hits["old"]; ok {
		t.Error("an expired window was not swept")
	}
}
