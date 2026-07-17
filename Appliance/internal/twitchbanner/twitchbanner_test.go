package twitchbanner

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationURLUsesExactRedirectAndScope(t *testing.T) {
	u, state, err := AuthorizationURL("client-id", "http://localhost:17563/twitch/callback")
	if err != nil || state == "" {
		t.Fatalf("url/state: %q %v", state, err)
	}
	if !strings.Contains(u, "client_id=client-id") || !strings.Contains(u, "redirect_uri=http%3A%2F%2Flocalhost%3A17563%2Ftwitch%2Fcallback") || !strings.Contains(u, "scope=bits%3Aread") || !strings.Contains(u, "response_type=code") {
		t.Fatalf("authorization URL missing contract: %s", u)
	}
}

func TestSubscribeCheerUsesSessionAndBroadcaster(t *testing.T) {
	var gotPath, gotAuth, gotClient string
	var got map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotClient = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Client-Id")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer s.Close()
	c := APIClient{ClientID: "cid", AccessToken: "token", BaseURL: s.URL, HTTP: s.Client()}
	if err := c.SubscribeCheer(context.Background(), "broadcaster", "session-123"); err != nil {
		t.Fatal(err)
	}
	cond := got["condition"].(map[string]any)
	transport := got["transport"].(map[string]any)
	if gotPath != "/helix/eventsub/subscriptions" || gotAuth != "Bearer token" || gotClient != "cid" || got["type"] != "channel.cheer" || got["version"] != "1" || cond["broadcaster_user_id"] != "broadcaster" || transport["method"] != "websocket" || transport["session_id"] != "session-123" {
		t.Fatalf("bad subscription request path=%s body=%#v", gotPath, got)
	}
}

func TestTokenExpiryNeedsRefresh(t *testing.T) {
	if !(Token{AccessToken: "token", ExpiresAt: time.Now().Add(30 * time.Second)}).NeedsRefresh(time.Now()) {
		t.Fatal("near-expiry token must refresh")
	}
	if (Token{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)}).NeedsRefresh(time.Now()) {
		t.Fatal("fresh token must not refresh")
	}
}

type fakePrinter struct {
	calls []string
	err   error
}

func (p *fakePrinter) Print(_ context.Context, text string) (PrintResult, error) {
	p.calls = append(p.calls, text)
	return PrintResult{JobID: "Rongta_F11_Media-10"}, p.err
}

func TestPrepareCheerThresholdAndFallback(t *testing.T) {
	if d := PrepareCheer(Cheer{Bits: 999, UserName: "Small", Message: "no"}); d.Qualifies {
		t.Fatal("999 bits must not qualify")
	}
	d := PrepareCheer(Cheer{Bits: 1000, UserName: "Cool_User", Message: "  "})
	if !d.Qualifies || d.Text != "THANK YOU COOL_USER" {
		t.Fatalf("unexpected fallback decision: %#v", d)
	}
	anon := PrepareCheer(Cheer{Bits: 1000, Anonymous: true})
	if anon.Text != "THANK YOU ANONYMOUS" {
		t.Fatalf("unexpected anonymous fallback: %q", anon.Text)
	}
}

func TestPrepareCheerSanitizesAndBoundsForBannerprint(t *testing.T) {
	d := PrepareCheer(Cheer{Bits: 1000, UserName: "Viewer", Message: "hello\nworld 😀 café " + strings.Repeat("word ", 30)})
	if !d.Qualifies {
		t.Fatal("qualifying cheer was rejected")
	}
	if strings.ContainsAny(d.Text, "\r\n\t") || strings.Contains(d.Text, "😀") {
		t.Fatalf("unsafe text remained: %q", d.Text)
	}
	if len([]byte(d.Text)) > 256 || len(strings.Fields(d.Text)) > 16 {
		t.Fatalf("banner bounds exceeded: bytes=%d words=%d", len([]byte(d.Text)), len(strings.Fields(d.Text)))
	}
}

func TestJournalReservationMakesDeliveryExactlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakePrinter{}
	proc := Processor{Journal: j, Printer: p}
	e := Envelope{MessageID: "event-1", Cheer: Cheer{Bits: 1000, UserName: "Cool", Message: "BIG MESSAGE"}}
	first, err := proc.Process(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	second, err := proc.Process(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Submitted || !second.Duplicate || len(p.calls) != 1 {
		t.Fatalf("first=%#v second=%#v calls=%v", first, second, p.calls)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte("\n")) < 2 || !bytes.Contains(data, []byte(`"state":"reserved"`)) || !bytes.Contains(data, []byte(`"state":"submitted"`)) {
		t.Fatalf("journal lacks durable transitions: %s", data)
	}
}

func TestJournalDoesNotRetryReservedEventAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := j.Reserve("event-ambiguous", Cheer{Bits: 1000}); err != nil || !ok {
		t.Fatalf("reserve: %v %v", ok, err)
	}
	j2, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakePrinter{}
	result, err := (Processor{Journal: j2, Printer: p}).Process(context.Background(), Envelope{MessageID: "event-ambiguous", Cheer: Cheer{Bits: 1000, Message: "DO NOT DUPLICATE"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Duplicate || len(p.calls) != 0 {
		t.Fatalf("ambiguous event retried: %#v calls=%v", result, p.calls)
	}
}

func TestJournalFailsClosedOnMalformedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"event_id\":\"possibly-reserved\""), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(path); err == nil {
		t.Fatal("malformed journal must prevent startup")
	}
}

func TestValidateIdentityRequiresClientBroadcasterAndBitsScope(t *testing.T) {
	good := TokenIdentity{ClientID: "cid", Login: "uwogoob", Scopes: []string{"bits:read"}}
	if err := good.Validate("cid", "uwogoob"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []TokenIdentity{
		{ClientID: "wrong", Login: "uwogoob", Scopes: []string{"bits:read"}},
		{ClientID: "cid", Login: "other", Scopes: []string{"bits:read"}},
		{ClientID: "cid", Login: "uwogoob", Scopes: []string{"chat:read"}},
	} {
		if err := bad.Validate("cid", "uwogoob"); err == nil {
			t.Fatalf("accepted invalid identity: %#v", bad)
		}
	}
}

func TestParseEventSubNotification(t *testing.T) {
	payload := map[string]any{
		"metadata": map[string]any{"message_id": "msg-123", "message_type": "notification", "subscription_type": "channel.cheer"},
		"payload":  map[string]any{"event": map[string]any{"is_anonymous": false, "user_name": "Cool_User", "message": "hello", "bits": float64(1000)}},
	}
	data, _ := json.Marshal(payload)
	env, ok, err := ParseNotification(data)
	if err != nil || !ok {
		t.Fatalf("parse: ok=%v err=%v", ok, err)
	}
	want := Envelope{MessageID: "msg-123", Cheer: Cheer{Bits: 1000, UserName: "Cool_User", Message: "hello"}}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("got %#v want %#v", env, want)
	}
	payload["metadata"].(map[string]any)["message_type"] = "session_keepalive"
	data, _ = json.Marshal(payload)
	if _, ok, err := ParseNotification(data); err != nil || ok {
		t.Fatalf("keepalive treated as event: ok=%v err=%v", ok, err)
	}
}

func TestOAuthCallbackValidatesStateAndCode(t *testing.T) {
	results := make(chan OAuthCallback, 1)
	h := OAuthCallbackHandler("expected-state", results)
	bad := httptest.NewRecorder()
	h.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/twitch/callback?state=wrong&code=x", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad state status=%d", bad.Code)
	}
	good := httptest.NewRecorder()
	h.ServeHTTP(good, httptest.NewRequest(http.MethodGet, "/twitch/callback?state=expected-state&code=auth-code", nil))
	if good.Code != http.StatusOK {
		t.Fatalf("good callback status=%d", good.Code)
	}
	select {
	case got := <-results:
		if got.Code != "auth-code" {
			t.Fatalf("code=%q", got.Code)
		}
	default:
		t.Fatal("callback result missing")
	}
}

func TestBannerPrinterUsesAutoLayoutAndOneInvocation(t *testing.T) {
	var gotName string
	var gotArgs []string
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName, gotArgs = name, append([]string(nil), args...)
		return []byte(`{"ok":true,"submitted":true,"job_id":"Rongta_F11_Media-11"}`), nil
	}
	p := BannerPrinter{Binary: "/usr/local/bin/bannerprint", Queue: "Rongta_F11_Media", Run: runner}
	result, err := p.Print(context.Background(), "MAKE THIS HUGE")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--queue", "Rongta_F11_Media", "--lines", "auto", "--", "MAKE THIS HUGE"}
	if gotName != p.Binary || !reflect.DeepEqual(gotArgs, want) || result.JobID != "Rongta_F11_Media-11" {
		t.Fatalf("name=%q args=%v result=%#v", gotName, gotArgs, result)
	}
}
