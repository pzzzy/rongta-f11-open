package main

import (
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/setupstate"
)

type memoryState struct{ state setupstate.State }

func (m *memoryState) Load() (setupstate.State, error)   { return m.state, nil }
func (m *memoryState) Save(state setupstate.State) error { m.state = state; return nil }

func newTestHandler(t *testing.T) (http.Handler, *memoryState) {
	t.Helper()
	state := &memoryState{state: setupstate.New()}
	h, err := newWizard(config{SetupCode: "correct-code", SecureCookie: true, Sessions: newSessionStore(rand.Reader), State: state, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return h, state
}

func TestPagesRequireLoginAndHealthDoesNot(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, path := range []string{"/", "/status"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		if r.Code != http.StatusSeeOther || r.Header().Get("Location") != "/login" {
			t.Errorf("%s: status/location = %d %q", path, r.Code, r.Header().Get("Location"))
		}
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if r.Code != http.StatusOK || strings.TrimSpace(r.Body.String()) != "ok" {
		t.Fatalf("health = %d %q", r.Code, r.Body.String())
	}
}

func TestLoginIssuesHardenedRandomSessionCookie(t *testing.T) {
	h, _ := newTestHandler(t)
	login := func() *http.Cookie {
		form := url.Values{"setup_code": {"correct-code"}}
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.ServeHTTP(r, req)
		if r.Code != http.StatusSeeOther {
			t.Fatalf("login status = %d: %s", r.Code, r.Body.String())
		}
		cookies := r.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("cookies = %v", cookies)
		}
		cookie := cookies[0]
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || len(cookie.Value) < 40 {
			t.Fatalf("weak cookie: %#v", cookie)
		}
		return cookie
	}
	first, second := login(), login()
	if first.Value == second.Value {
		t.Fatal("session cookies were reused")
	}
}

func TestHTTPSetupModeCookieWorksWithoutTLS(t *testing.T) {
	state := &memoryState{state: setupstate.New()}
	h, err := newWizard(config{SetupCode: "correct-code", SecureCookie: false, Sessions: newSessionStore(rand.Reader), State: state, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"setup_code": {"correct-code"}}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(r, req)
	cookies := r.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("HTTP setup cookie=%#v", cookies)
	}
}

func TestInvalidLoginIsGenericAndDoesNotSetCookie(t *testing.T) {
	h, _ := newTestHandler(t)
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("setup_code=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(r, req)
	if r.Code != http.StatusUnauthorized || len(r.Result().Cookies()) != 0 || strings.Contains(r.Body.String(), "wrong") {
		t.Fatalf("invalid login response = %d cookies=%v body=%q", r.Code, r.Result().Cookies(), r.Body.String())
	}
}

func TestArbitraryCheckpointEndpointIsDisabled(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(method, "/checkpoint", nil))
		if r.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s checkpoint status = %d", method, r.Code)
		}
	}
}

func TestSecurityHeadersNoExternalAssetsAndBoundedBody(t *testing.T) {
	h, _ := newTestHandler(t)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/login", nil))
	for header, want := range map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
	} {
		if !strings.Contains(r.Header().Get(header), want) {
			t.Errorf("%s = %q, want containing %q", header, r.Header().Get(header), want)
		}
	}
	body := r.Body.String()
	if strings.Contains(body, "http://") || strings.Contains(body, "https://") || strings.Contains(body, "<script") {
		t.Fatalf("login contains external or script asset: %s", body)
	}

	tooLarge := strings.Repeat("x", maxRequestBytes+1)
	r = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("setup_code="+tooLarge))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(r, req)
	if r.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request status = %d", r.Code)
	}
}

func TestMethodRestrictions(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, tc := range []struct{ method, path string }{{http.MethodPut, "/login"}, {http.MethodPost, "/healthz"}, {http.MethodPost, "/status"}, {http.MethodGet, "/checkpoint"}} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(tc.method, tc.path, nil))
		if r.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d", tc.method, tc.path, r.Code)
		}
	}
}

func authenticate(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	form := url.Values{"setup_code": {"correct-code"}}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(r, req)
	if len(r.Result().Cookies()) != 1 {
		t.Fatalf("login failed: %d", r.Code)
	}
	return r.Result().Cookies()[0]
}

func extractCSRF(t *testing.T, body string) string {
	t.Helper()
	const marker = `name="csrf_token" value="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("no csrf token in %s", body)
	}
	start += len(marker)
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		t.Fatal("unterminated csrf token")
	}
	return body[start : start+end]
}
