package main

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/setupstate"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/twitchdevice"
)

type fakeHelper struct {
	responses map[string]helperResponse
	calls     []helperRequest
}

func (f *fakeHelper) Call(_ context.Context, req helperRequest) (helperResponse, error) {
	f.calls = append(f.calls, req)
	return f.responses[req.Op], nil
}

type fakeTwitch struct {
	flow   twitchdevice.Flow
	result twitchdevice.Result
	state  twitchdevice.State
}

func (f *fakeTwitch) Start(context.Context, string) (twitchdevice.Flow, error) { return f.flow, nil }
func (f *fakeTwitch) Poll(context.Context, twitchdevice.Flow) (twitchdevice.Result, twitchdevice.State, error) {
	return f.result, f.state, nil
}

type fakeSaver struct {
	result twitchdevice.Result
	calls  int
}

func (f *fakeSaver) Save(result twitchdevice.Result) error { f.calls++; f.result = result; return nil }

func guidedHandler(t *testing.T) (http.Handler, *memoryState, *fakeHelper, *fakeTwitch, *fakeSaver) {
	t.Helper()
	st := &memoryState{state: setupstate.New()}
	hp := &fakeHelper{responses: map[string]helperResponse{
		"wifi_status":       {OK: true, Data: map[string]any{"connected": true, "recovery_ap": false}},
		"wifi_connect":      {OK: true, Data: map[string]any{"connected": true}},
		"printer_probe":     {OK: true, Printer: helperPrinter{Present: true, Model: "Rongta F11"}},
		"printer_configure": {OK: true, Data: map[string]any{"configured": true, "queue": "Rongta_F11_Media"}},
		"service_status":    {OK: true, Data: map[string]any{"service": "twitch-banner", "status": "active", "eventsub_ready": true}},
		"preview_test":      {OK: true, Data: map[string]any{"previews": true}},
		"physical_test":     {OK: true, Data: map[string]any{"job_id": "Rongta_F11_Media-9"}},
		"twitch_install":    {OK: true, Data: map[string]any{"installed": true}},
	}}
	tw := &fakeTwitch{flow: twitchdevice.Flow{ClientID: "publicclient1234567890", DeviceCode: "device-secret", VerificationURI: "https://www.twitch.tv/activate", UserCode: "ABCD-EFGH"}, state: twitchdevice.Pending}
	sv := &fakeSaver{}
	h, err := newWizard(config{SetupCode: "correct-code", Sessions: newSessionStore(rand.Reader), State: st, Helper: hp, Twitch: tw, Saver: sv, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return h, st, hp, tw, sv
}

func postAction(t *testing.T, h http.Handler, cookie *http.Cookie, path string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	page := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(page, req)
	values.Set("csrf_token", extractCSRF(t, page.Body.String()))
	r := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	h.ServeHTTP(r, req)
	return r
}

func TestExistingStationConnectionCompletesNetworkWithoutCredentials(t *testing.T) {
	h, state, helper, _, _ := guidedHandler(t)
	cookie := authenticate(t, h)
	if r := postAction(t, h, cookie, "/action/welcome", url.Values{}); r.Code != http.StatusSeeOther {
		t.Fatal(r.Code)
	}
	r := postAction(t, h, cookie, "/action/network", url.Values{"use_current": {"yes"}})
	if r.Code != http.StatusSeeOther || !state.state.Completed(setupstate.CheckpointNetwork) {
		t.Fatalf("network status=%d state=%#v", r.Code, state.state)
	}
	if helper.calls[len(helper.calls)-1].Op != "wifi_status" {
		t.Fatalf("calls=%+v", helper.calls)
	}
}

func TestGuidedStagesAdvanceOnlyAfterVerifiedSuccess(t *testing.T) {
	h, state, helper, _, _ := guidedHandler(t)
	cookie := authenticate(t, h)
	if r := postAction(t, h, cookie, "/action/network", url.Values{"ssid": {"Home"}, "wifi_password": {"password123"}}); r.Code != http.StatusConflict || state.state.Completed(setupstate.CheckpointNetwork) {
		t.Fatalf("network bypass status=%d state=%#v", r.Code, state.state)
	}
	if r := postAction(t, h, cookie, "/action/welcome", url.Values{}); r.Code != http.StatusSeeOther {
		t.Fatal(r.Code)
	}
	if r := postAction(t, h, cookie, "/action/network", url.Values{"ssid": {"Home"}, "wifi_password": {"password123"}}); r.Code != http.StatusSeeOther || !state.state.Completed(setupstate.CheckpointNetwork) {
		t.Fatalf("network status=%d state=%#v", r.Code, state.state)
	}
	helper.responses["printer_configure"] = helperResponse{OK: false}
	if r := postAction(t, h, cookie, "/action/printer", url.Values{}); r.Code != http.StatusBadGateway || state.state.Completed(setupstate.CheckpointPrinter) {
		t.Fatalf("printer improperly advanced %d", r.Code)
	}
}

func TestTwitchDeviceCodeAndSecretsNeverRender(t *testing.T) {
	h, state, _, tw, saver := guidedHandler(t)
	cookie := authenticate(t, h)
	for _, action := range []string{"welcome", "network", "printer"} {
		values := url.Values{}
		if action == "network" {
			values = url.Values{"ssid": {"Home"}, "wifi_password": {"wifi-secret-123"}}
		}
		r := postAction(t, h, cookie, "/action/"+action, values)
		if r.Code != http.StatusSeeOther || strings.Contains(r.Body.String(), "wifi-secret-123") {
			t.Fatalf("%s=%d", action, r.Code)
		}
	}
	r := postAction(t, h, cookie, "/action/twitch/start", url.Values{"client_id": {"publicclient1234567890"}})
	if r.Code != http.StatusSeeOther {
		t.Fatal(r.Code)
	}
	page := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(page, req)
	if strings.Contains(page.Body.String(), "device-secret") || !strings.Contains(page.Body.String(), "ABCD-EFGH") {
		t.Fatalf("unsafe page %s", page.Body.String())
	}
	tw.state = twitchdevice.Authorized
	tw.result = twitchdevice.Result{Token: twitchdevice.Token{AccessToken: "access-secret", RefreshToken: "refresh-secret", Scope: []string{"bits:read", "user:read:chat"}}, Identity: twitchdevice.Identity{ClientID: "publicclient1234567890", Login: "login", UserID: "123", Scopes: []string{"bits:read", "user:read:chat"}}}
	r = postAction(t, h, cookie, "/action/twitch/poll", url.Values{})
	if r.Code != http.StatusSeeOther || !state.state.Completed(setupstate.CheckpointTwitch) || saver.calls != 1 {
		t.Fatalf("poll=%d state=%#v saves=%d", r.Code, state.state, saver.calls)
	}
	if strings.Contains(r.Body.String(), "access-secret") || strings.Contains(r.Body.String(), "refresh-secret") {
		t.Fatal("token rendered")
	}
}

func TestPhysicalPrintAmbiguityCannotBeRetried(t *testing.T) {
	h, state, helper, _, _ := guidedHandler(t)
	cookie := authenticate(t, h)
	for _, checkpoint := range []setupstate.Checkpoint{setupstate.CheckpointWelcome, setupstate.CheckpointNetwork, setupstate.CheckpointPrinter, setupstate.CheckpointTwitch, setupstate.CheckpointEventSub, setupstate.CheckpointPreview} {
		if err := state.state.Complete(checkpoint, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	helper.responses["physical_test"] = helperResponse{OK: false}
	first := postAction(t, h, cookie, "/action/physical-test", url.Values{"confirm_physical_print": {"yes"}})
	if first.Code != http.StatusBadGateway || !state.state.Completed(setupstate.CheckpointPhysicalAttempted) {
		t.Fatalf("first=%d state=%#v", first.Code, state.state)
	}
	calls := len(helper.calls)
	second := postAction(t, h, cookie, "/action/physical-test", url.Values{"confirm_physical_print": {"yes"}})
	if second.Code != http.StatusConflict || len(helper.calls) != calls {
		t.Fatalf("second=%d calls=%v", second.Code, helper.calls)
	}
}

func TestMutationsRequireCSRFAndBoundedBodies(t *testing.T) {
	h, _, _, _, _ := guidedHandler(t)
	cookie := authenticate(t, h)
	for _, path := range []string{"/action/welcome", "/action/network", "/action/printer", "/action/twitch/start", "/action/twitch/poll", "/action/eventsub", "/action/preview", "/action/physical-test", "/action/complete"} {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("csrf_token=bad"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		h.ServeHTTP(r, req)
		if r.Code != http.StatusForbidden {
			t.Errorf("%s csrf=%d", path, r.Code)
		}
	}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/action/welcome", strings.NewReader("csrf_token="+strings.Repeat("x", maxRequestBytes+1)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	h.ServeHTTP(r, req)
	if r.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large=%d", r.Code)
	}
}

func TestHelperResponseStrictAndPSKNotInError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"ok":true,"unknown":"secret-psk"}`) }))
	defer s.Close()
	_ = s // Unix client strict decoding is tested through decodeHelperResponse.
	if _, err := decodeHelperResponse(strings.NewReader(`{"ok":true,"unknown":"secret-psk"}`)); err == nil || strings.Contains(err.Error(), "secret-psk") {
		t.Fatalf("err=%v", err)
	}
}
