package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/setupstate"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/twitchdevice"
)

const (
	cookieName          = "f11_setup_session"
	maxRequestBytes     = 8 << 10
	maxHelperBytes      = 32 << 10
	sessionLifetime     = 30 * time.Minute
	defaultHelperSocket = "/run/f11-setup/helper.sock"
)

type stateStore interface {
	Load() (setupstate.State, error)
	Save(setupstate.State) error
}
type helperAPI interface {
	Call(context.Context, helperRequest) (helperResponse, error)
}
type twitchAPI interface {
	Start(context.Context, string) (twitchdevice.Flow, error)
	Poll(context.Context, twitchdevice.Flow) (twitchdevice.Result, twitchdevice.State, error)
}
type twitchSaver interface {
	Save(twitchdevice.Result) error
}

type config struct {
	SetupCode    string
	SecureCookie bool
	Sessions     *sessionStore
	State        stateStore
	Logger       *slog.Logger
	Helper       helperAPI
	Twitch       twitchAPI
	Saver        twitchSaver
	ClientID     string
}
type session struct {
	csrf      string
	expiresAt time.Time
	flow      *twitchdevice.Flow
	slowDowns int
}
type sessionStore struct {
	mu       sync.Mutex
	random   io.Reader
	sessions map[string]session
	now      func() time.Time
}

func newSessionStore(r io.Reader) *sessionStore {
	return &sessionStore{random: r, sessions: map[string]session{}, now: time.Now}
}
func randomToken(r io.Reader) (string, error) {
	b := make([]byte, 32)
	if _, e := io.ReadFull(r, b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s *sessionStore) create() (string, session, error) {
	t, e := randomToken(s.random)
	if e != nil {
		return "", session{}, e
	}
	c, e := randomToken(s.random)
	if e != nil {
		return "", session{}, e
	}
	x := session{csrf: c, expiresAt: s.now().Add(sessionLifetime)}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if !v.expiresAt.After(s.now()) {
			delete(s.sessions, k)
		}
	}
	s.sessions[t] = x
	return t, x, nil
}
func (s *sessionStore) get(k string) (session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[k]
	if !ok || !v.expiresAt.After(s.now()) {
		delete(s.sessions, k)
		return session{}, false
	}
	return v, true
}
func (s *sessionStore) setFlow(k string, f twitchdevice.Flow) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[k]
	if !ok {
		return false
	}
	v.flow = &f
	v.slowDowns = 0
	s.sessions[k] = v
	return true
}
func (s *sessionStore) flow(k string) (twitchdevice.Flow, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[k]
	if !ok || v.flow == nil {
		return twitchdevice.Flow{}, 0, false
	}
	return *v.flow, v.slowDowns, true
}
func (s *sessionStore) slow(k string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.sessions[k]
	v.slowDowns++
	s.sessions[k] = v
}
func (s *sessionStore) clearFlow(k string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.sessions[k]
	v.flow = nil
	s.sessions[k] = v
}

type helperRequest struct {
	Op           string   `json:"op"`
	SSID         string   `json:"ssid,omitempty"`
	PSK          string   `json:"psk,omitempty"`
	Model        string   `json:"model,omitempty"`
	ClientID     string   `json:"client_id,omitempty"`
	Login        string   `json:"login,omitempty"`
	UserID       string   `json:"user_id,omitempty"`
	AccessToken  string   `json:"access_token,omitempty"`
	RefreshToken string   `json:"refresh_token,omitempty"`
	ExpiresAt    string   `json:"expires_at,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}
type helperPrinter struct {
	Model        string `json:"model"`
	Present      bool   `json:"present"`
	SerialSuffix string `json:"serial_suffix,omitempty"`
}
type helperError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}
type helperResponse struct {
	OK      bool           `json:"ok"`
	Error   *helperError   `json:"error,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
	Printer helperPrinter  `json:"printer,omitempty"`
}
type unixHelper struct {
	path    string
	timeout time.Duration
}

func (h unixHelper) Call(ctx context.Context, req helperRequest) (helperResponse, error) {
	d := net.Dialer{Timeout: h.timeout}
	c, e := d.DialContext(ctx, "unix", h.path)
	if e != nil {
		return helperResponse{}, errors.New("setup helper unavailable")
	}
	defer c.Close()
	deadline := time.Now().Add(h.timeout)
	_ = c.SetDeadline(deadline)
	if e = json.NewEncoder(c).Encode(req); e != nil {
		return helperResponse{}, errors.New("setup helper request failed")
	}
	return decodeHelperResponse(c)
}
func decodeHelperResponse(r io.Reader) (helperResponse, error) {
	var out helperResponse
	d := json.NewDecoder(io.LimitReader(r, maxHelperBytes+1))
	d.DisallowUnknownFields()
	if e := d.Decode(&out); e != nil {
		return out, errors.New("invalid setup helper response")
	}
	var extra any
	if e := d.Decode(&extra); !errors.Is(e, io.EOF) {
		return out, errors.New("invalid setup helper response")
	}
	if out.OK == (out.Error != nil) {
		return out, errors.New("invalid setup helper response")
	}
	return out, nil
}

type fileSaver struct{ token, environment string }

func (f fileSaver) Save(r twitchdevice.Result) error {
	return twitchdevice.Save(f.token, f.environment, r)
}

type helperSaver struct{ helper helperAPI }

func (h helperSaver) Save(r twitchdevice.Result) error {
	resp, err := h.helper.Call(context.Background(), helperRequest{Op: "twitch_install", ClientID: r.Identity.ClientID, Login: r.Identity.Login, UserID: r.Identity.UserID, AccessToken: r.Token.AccessToken, RefreshToken: r.Token.RefreshToken, ExpiresAt: r.Token.ExpiresAt.UTC().Format(time.RFC3339), Scopes: append([]string(nil), r.Token.Scope...)})
	if err != nil || !resp.OK || resp.Data["installed"] != true {
		return errors.New("Twitch installation failed")
	}
	return nil
}

type wizard struct {
	setupCode    string
	secureCookie bool
	sessions     *sessionStore
	state        stateStore
	logger       *slog.Logger
	helper       helperAPI
	twitch       twitchAPI
	saver        twitchSaver
	clientID     string
}

func newWizard(c config) (http.Handler, error) {
	if strings.TrimSpace(c.SetupCode) == "" || c.Sessions == nil || c.State == nil {
		return nil, errors.New("wizard dependencies are required")
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Helper == nil {
		c.Helper = unixHelper{path: defaultHelperSocket, timeout: 10 * time.Second}
	}
	if c.Twitch == nil {
		c.Twitch = twitchdevice.New(nil, "")
	}
	if c.Saver == nil {
		c.Saver = helperSaver{helper: c.Helper}
	}
	a := &wizard{c.SetupCode, c.SecureCookie, c.Sessions, c.State, c.Logger, c.Helper, c.Twitch, c.Saver, c.ClientID}
	m := http.NewServeMux()
	m.HandleFunc("/login", a.login)
	m.HandleFunc("/healthz", a.health)
	m.HandleFunc("/", a.home)
	m.HandleFunc("/status", a.status)
	m.HandleFunc("/checkpoint", func(w http.ResponseWriter, r *http.Request) { methodNotAllowed(w, http.MethodPost) })
	for _, x := range []string{"welcome", "network", "printer", "twitch/start", "twitch/poll", "eventsub", "preview", "physical-test", "complete"} {
		x := x
		m.HandleFunc("/action/"+x, func(w http.ResponseWriter, r *http.Request) { a.action(w, r, x) })
	}
	return securityHeaders(m), nil
}
func securityHeaders(n http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		n.ServeHTTP(w, r)
	})
}
func (a *wizard) login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		render(w, pageData{Title: "Sign in", Login: true})
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "GET, POST")
		return
	}
	if parseBoundedForm(w, r) != nil {
		return
	}
	p := r.Form.Get("setup_code")
	if len(p) != len(a.setupCode) || subtle.ConstantTimeCompare([]byte(p), []byte(a.setupCode)) != 1 {
		http.Error(w, "Setup code not accepted", 401)
		return
	}
	t, _, e := a.sessions.create()
	if e != nil {
		a.logger.Error("session creation failed")
		http.Error(w, "Unable to start session", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: t, Path: "/", MaxAge: int(sessionLifetime.Seconds()), HttpOnly: true, Secure: a.secureCookie, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", 303)
}
func (a *wizard) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "ok\n")
}
func (a *wizard) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	s, k, ok := a.authorize(w, r)
	if !ok {
		return
	}
	st, e := a.state.Load()
	if e != nil {
		http.Error(w, "Setup state is unavailable", 500)
		return
	}
	var pub *twitchdevice.PublicFlow
	if f, _, yes := a.sessions.flow(k); yes {
		p := f.Public()
		pub = &p
	}
	render(w, pageData{Title: "Guided setup", Guide: true, CSRF: s.csrf, State: st, Flow: pub, ClientID: a.clientID})
}
func (a *wizard) status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	if _, _, ok := a.authorize(w, r); !ok {
		return
	}
	http.Redirect(w, r, "/", 303)
}
func (a *wizard) authorize(w http.ResponseWriter, r *http.Request) (session, string, bool) {
	c, e := r.Cookie(cookieName)
	if e != nil {
		http.Redirect(w, r, "/login", 303)
		return session{}, "", false
	}
	s, ok := a.sessions.get(c.Value)
	if !ok {
		http.Redirect(w, r, "/login", 303)
		return session{}, "", false
	}
	return s, c.Value, true
}
func (a *wizard) action(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	s, key, ok := a.authorize(w, r)
	if !ok {
		return
	}
	if parseBoundedForm(w, r) != nil {
		return
	}
	if !constantEqual(r.Form.Get("csrf_token"), s.csrf) {
		http.Error(w, "Invalid request token", 403)
		return
	}
	st, e := a.state.Load()
	if e != nil {
		generic(w, 500)
		return
	}
	complete := func(cp setupstate.Checkpoint) bool {
		if e := st.Complete(cp, time.Now()); e != nil || a.state.Save(st) != nil {
			generic(w, 500)
			return false
		}
		return true
	}
	need := func(cp setupstate.Checkpoint) bool {
		if !st.Completed(cp) {
			http.Error(w, "Complete the previous verified step first", 409)
			return false
		}
		return true
	}
	switch name {
	case "welcome":
		if complete(setupstate.CheckpointWelcome) {
			redirect(w, r)
		}
		return
	case "network":
		if !need(setupstate.CheckpointWelcome) {
			return
		}
		ssid, psk := r.Form.Get("ssid"), r.Form.Get("wifi_password")
		if len([]byte(ssid)) < 1 || len([]byte(ssid)) > 32 || len(psk) < 8 || len(psk) > 63 {
			http.Error(w, "Enter a valid Wi-Fi name and WPA password", 400)
			return
		}
		resp, e := a.helper.Call(r.Context(), helperRequest{Op: "wifi_connect", SSID: ssid, PSK: psk})
		if e != nil || !resp.OK || resp.Data["connected"] != true {
			generic(w, 502)
			return
		}
		if complete(setupstate.CheckpointNetwork) {
			redirect(w, r)
		}
		return
	case "printer":
		if !need(setupstate.CheckpointNetwork) {
			return
		}
		probe, e := a.helper.Call(r.Context(), helperRequest{Op: "printer_probe"})
		if e != nil || !probe.OK || !probe.Printer.Present {
			generic(w, 502)
			return
		}
		cfg, e := a.helper.Call(r.Context(), helperRequest{Op: "printer_configure", Model: "Rongta_F11_Media"})
		if e != nil || !cfg.OK || cfg.Data["configured"] != true || cfg.Data["queue"] != "Rongta_F11_Media" {
			generic(w, 502)
			return
		}
		if complete(setupstate.CheckpointPrinter) {
			redirect(w, r)
		}
		return
	case "twitch/start":
		if !need(setupstate.CheckpointPrinter) {
			return
		}
		id := strings.TrimSpace(r.Form.Get("client_id"))
		f, e := a.twitch.Start(r.Context(), id)
		if e != nil {
			generic(w, 502)
			return
		}
		a.sessions.setFlow(key, f)
		redirect(w, r)
		return
	case "twitch/poll":
		if !need(setupstate.CheckpointPrinter) {
			return
		}
		f, sl, yes := a.sessions.flow(key)
		if !yes {
			http.Error(w, "Start Twitch authorization first", 409)
			return
		}
		if sl > 0 {
			f.Interval += time.Duration(sl) * time.Second
		}
		result, state, e := a.twitch.Poll(r.Context(), f)
		if e != nil {
			a.sessions.clearFlow(key)
			generic(w, 502)
			return
		}
		if state == twitchdevice.Pending {
			http.Error(w, "Twitch authorization is still pending", 409)
			return
		}
		if state == twitchdevice.SlowDown {
			a.sessions.slow(key)
			http.Error(w, "Wait longer before retrying", 429)
			return
		}
		if state != twitchdevice.Authorized || a.saver.Save(result) != nil {
			generic(w, 502)
			return
		}
		a.sessions.clearFlow(key)
		if complete(setupstate.CheckpointTwitch) {
			redirect(w, r)
		}
		return
	case "eventsub":
		if !need(setupstate.CheckpointTwitch) {
			return
		}
		resp, e := a.helper.Call(r.Context(), helperRequest{Op: "service_status"})
		if e != nil || !resp.OK || resp.Data["status"] != "active" || resp.Data["eventsub_ready"] != true {
			generic(w, 502)
			return
		}
		if complete(setupstate.CheckpointEventSub) {
			redirect(w, r)
		}
		return
	case "preview":
		if !need(setupstate.CheckpointEventSub) {
			return
		}
		resp, e := a.helper.Call(r.Context(), helperRequest{Op: "preview_test"})
		if e != nil || !resp.OK || resp.Data["previews"] != true {
			generic(w, 502)
			return
		}
		if complete(setupstate.CheckpointPreview) {
			redirect(w, r)
		}
		return
	case "physical-test":
		if !need(setupstate.CheckpointPreview) {
			return
		}
		if st.Completed(setupstate.CheckpointPhysicalAttempted) {
			http.Error(w, "A physical print was already attempted. Check the printer and CUPS before any manual retry.", 409)
			return
		}
		if r.Form.Get("confirm_physical_print") != "yes" {
			http.Error(w, "Explicit print confirmation required", 400)
			return
		}
		probe, e := a.helper.Call(r.Context(), helperRequest{Op: "printer_probe"})
		if e != nil || !probe.OK || !probe.Printer.Present {
			generic(w, 502)
			return
		}
		if !complete(setupstate.CheckpointPhysicalAttempted) {
			return
		}
		printed, e := a.helper.Call(r.Context(), helperRequest{Op: "physical_test"})
		if e != nil || !printed.OK || printed.Data["job_id"] == "" {
			generic(w, 502)
			return
		}
		redirect(w, r)
		return
	case "complete":
		if !need(setupstate.CheckpointPreview) {
			return
		}
		if complete(setupstate.CheckpointComplete) {
			redirect(w, r)
		}
		return
	default:
		http.NotFound(w, r)
	}
}
func redirect(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", 303) }
func generic(w http.ResponseWriter, status int) {
	http.Error(w, "The verified operation failed. Check the appliance and try again.", status)
}
func constantEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func parseBoundedForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	if e := r.ParseForm(); e != nil {
		var m *http.MaxBytesError
		if errors.As(e, &m) {
			http.Error(w, "Request too large", 413)
		} else {
			http.Error(w, "Invalid form", 400)
		}
		return e
	}
	return nil
}
func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	http.Error(w, "Method not allowed", 405)
}

type pageData struct {
	Title        string
	Login, Guide bool
	CSRF         string
	ClientID     string
	State        setupstate.State
	Flow         *twitchdevice.PublicFlow
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} · F11 setup</title><style>:root{font-family:system-ui;line-height:1.5}body{margin:0;background:#eef2f7;color:#18212f}main{max-width:42rem;margin:auto;padding:1rem}section{background:white;border-radius:1rem;padding:2rem}input,button{box-sizing:border-box;width:100%;min-height:3rem;margin:.5rem 0;padding:.7rem}button{background:#4b35d1;color:white;border:0;border-radius:.6rem;font-weight:700}.step{border-top:1px solid #ccd3dd;padding:1rem 0}.done{color:#176b3a;font-weight:700}.warning{border:2px solid #b44;padding:.7rem}</style></head><body><main><section>{{if .Login}}<h1>Welcome</h1><form method="post" action="/login"><label>Setup code<input name="setup_code" type="password" maxlength="128" required></label><button>Continue</button></form>{{end}}{{if .Guide}}<h1>Guided F11 setup</h1><p>Each stage advances only after its operation succeeds.</p>
<div class="step"><h2>1. Welcome</h2>{{if .State.Completed "welcome"}}<span class="done">Done</span>{{else}}<form method="post" action="/action/welcome"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><button>Begin setup</button></form>{{end}}</div>
<div class="step"><h2>2. Connect Wi-Fi</h2>{{if .State.Completed "network"}}<span class="done">Connected</span>{{else}}<p>Enter the home Wi-Fi details. The temporary setup network may disconnect after this succeeds; reconnect your phone to home Wi-Fi and reopen <strong>http://f11-setup.local:8080/</strong>.</p><form method="post" action="/action/network"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><label>Wi-Fi name<input name="ssid" maxlength="32" required autocomplete="off"></label><label>Wi-Fi password<input name="wifi_password" type="password" minlength="8" maxlength="63" required autocomplete="new-password"></label><button>Connect and verify</button></form>{{end}}</div>
<div class="step"><h2>3. Printer</h2><p>Probe one attached F11 and configure the canonical Rongta_F11_Media queue.</p>{{if .State.Completed "printer"}}<span class="done">Verified</span>{{else}}<form method="post" action="/action/printer"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><button>Probe and configure</button></form>{{end}}</div>
<div class="step"><h2>4. Twitch</h2>{{if .State.Completed "twitch"}}<span class="done">Authorized</span>{{else}}{{if .Flow}}<p>Open <strong>{{.Flow.VerificationURI}}</strong> on a trusted device and enter code <strong>{{.Flow.UserCode}}</strong>.</p><form method="post" action="/action/twitch/poll"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><button>I've approved it — check Twitch</button></form>{{else}}<p>Enter the public Client ID from your Twitch application. No client secret is used.</p><form method="post" action="/action/twitch/start"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><label>Client ID<input name="client_id" maxlength="128" required></label><button>Start device authorization</button></form>{{end}}{{end}}</div>
<div class="step"><h2>5. EventSub readiness</h2><p>Verify the appliance service is active.</p>{{if .State.Completed "eventsub"}}<span class="done">Ready</span>{{else}}<form method="post" action="/action/eventsub"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><button>Check service</button></form>{{end}}</div>
<div class="step"><h2>6. No-paper previews</h2><p>Review banner, gift, and raid behavior without sending paper.</p>{{if .State.Completed "preview"}}<span class="done">Reviewed</span>{{else}}<form method="post" action="/action/preview"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><button>Preview reviewed</button></form>{{end}}</div>
<div class="step"><h2>Optional physical test</h2><p class="warning"><strong>Warning:</strong> this opt-in test can use paper. The printer must be attached. This build verifies readiness but does not submit an arbitrary print through the privileged helper.</p><form method="post" action="/action/physical-test"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><label><input type="checkbox" name="confirm_physical_print" value="yes" required> I understand paper may be used</label><button>Verify physical-test readiness</button></form></div>
<div class="step"><h2>7. Completion</h2>{{if .State.Completed "complete"}}<span class="done">Setup complete</span>{{else}}<form method="post" action="/action/complete"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><button>Finish setup</button></form>{{end}}</div>{{end}}</section></main></body></html>`))

func render(w http.ResponseWriter, d pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if e := pageTemplate.Execute(w, d); e != nil {
		slog.Error("render failed")
	}
}

func main() {
	listen := flag.String("listen", "0.0.0.0:8080", "")
	state := flag.String("state", "/var/lib/f11-setup/state.json", "")
	codeFile := flag.String("setup-code-file", "/run/credentials/f11-setup-wizard.service/setup-code", "")
	clientFile := flag.String("twitch-client-id-file", "/etc/f11-setup/twitch-client-id", "")
	helper := flag.String("helper-socket", defaultHelperSocket, "")

	secure := flag.Bool("secure-cookie", false, "")
	flag.Parse()
	code, e := os.ReadFile(*codeFile)
	if e != nil || strings.TrimSpace(string(code)) == "" {
		slog.Error("cannot read setup code credential")
		os.Exit(1)
	}
	clientID := ""
	if f, e := os.Open(*clientFile); e == nil {
		scan := bufio.NewScanner(io.LimitReader(f, 256))
		if scan.Scan() {
			clientID = strings.TrimSpace(scan.Text())
		}
		f.Close()
	}
	tw := twitchdevice.New(nil, "")
	sessions := newSessionStore(rand.Reader)
	h, e := newWizard(config{SetupCode: strings.TrimSpace(string(code)), SecureCookie: *secure, Sessions: sessions, State: setupstate.NewStore(*state), Helper: unixHelper{*helper, 10 * time.Second}, Twitch: tw})
	if e != nil {
		os.Exit(1)
	}
	if clientID != "" {
		_ = twitchdevice.ValidateClientID(clientID)
	}
	srv := http.Server{Addr: *listen, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	slog.Info("setup wizard listening", "address", *listen)
	if e = srv.ListenAndServe(); !errors.Is(e, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
