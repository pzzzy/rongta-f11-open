package twitchdevice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStartUsesExactPublicDeviceGrantAndHidesDeviceCode(t *testing.T) {
	var got url.Values
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/device" || r.Method != http.MethodPost {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		_ = r.ParseForm()
		got = r.Form
		io.WriteString(w, `{"device_code":"private-device","user_code":"ABCD-EFGH","verification_uri":"https://www.twitch.tv/activate","expires_in":600,"interval":5}`)
	}))
	defer s.Close()
	c := New(s.Client(), s.URL)
	flow, err := c.Start(context.Background(), "publicclient1234567890")
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("client_id") != "publicclient1234567890" || got.Get("scopes") != Scope || len(got) != 2 {
		t.Fatalf("form=%v", got)
	}
	public := flow.Public()
	b, _ := json.Marshal(public)
	if strings.Contains(string(b), "private-device") || public.UserCode != "ABCD-EFGH" || flow.DeviceCode != "private-device" {
		t.Fatalf("flow=%#v public=%s", flow, b)
	}
}

func TestPollOAuthErrorsAndSuccessfulValidation(t *testing.T) {
	responses := []struct {
		status int
		body   string
	}{
		{400, `{"status":400,"message":"authorization_pending"}`},
		{400, `{"status":400,"message":"slow_down"}`},
		{200, `{"access_token":"access-secret","refresh_token":"refresh-secret","expires_in":3600,"scope":["bits:read","user:read:chat"],"token_type":"bearer"}`},
	}
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/validate" {
			if r.Header.Get("Authorization") != "OAuth access-secret" {
				t.Fatal("missing OAuth validation header")
			}
			io.WriteString(w, `{"client_id":"publicclient1234567890","login":"some_login","user_id":"12345","scopes":["bits:read","user:read:chat"]}`)
			return
		}
		resp := responses[calls]
		calls++
		w.WriteHeader(resp.status)
		io.WriteString(w, resp.body)
	}))
	defer s.Close()
	c := New(s.Client(), s.URL)
	flow := Flow{ClientID: "publicclient1234567890", DeviceCode: "device-secret", ExpiresAt: time.Now().Add(time.Minute), Interval: time.Second}
	if _, state, err := c.Poll(context.Background(), flow); err != nil || state != Pending {
		t.Fatalf("pending state=%s err=%v", state, err)
	}
	if _, state, err := c.Poll(context.Background(), flow); err != nil || state != SlowDown {
		t.Fatalf("slow state=%s err=%v", state, err)
	}
	result, state, err := c.Poll(context.Background(), flow)
	if err != nil || state != Authorized || result.Identity.UserID != "12345" || result.Identity.Login != "some_login" {
		t.Fatalf("result=%#v state=%s err=%v", result, state, err)
	}
}

func TestRejectsWrongScopesUnknownFieldsOversizeAndExpired(t *testing.T) {
	for name, body := range map[string]string{
		"wrong scopes": `{"device_code":"d","user_code":"u","verification_uri":"https://x","expires_in":60,"interval":1,"extra":true}`,
		"oversize":     strings.Repeat("x", MaxResponseBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
			defer s.Close()
			_, err := New(s.Client(), s.URL).Start(context.Background(), "publicclient1234567890")
			if err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
	c := New(http.DefaultClient, "https://invalid.example")
	_, state, err := c.Poll(context.Background(), Flow{ExpiresAt: time.Now().Add(-time.Second)})
	if err == nil || state != Expired {
		t.Fatalf("state=%s err=%v", state, err)
	}
}

func TestSaveAtomicallyWritesImmutableIdentityAndRootProtectedFiles(t *testing.T) {
	d := t.TempDir()
	tokenPath := filepath.Join(d, "state", "token.json")
	envPath := filepath.Join(d, "etc", "environment")
	result := Result{
		Token:    Token{AccessToken: "access-secret", RefreshToken: "refresh-secret", Scope: []string{"bits:read", "user:read:chat"}, TokenType: "bearer", ExpiresAt: time.Unix(2000000000, 0).UTC()},
		Identity: Identity{ClientID: "publicclient1234567890", Login: "some_login", UserID: "12345", Scopes: []string{"bits:read", "user:read:chat"}},
	}
	if err := Save(tokenPath, envPath, result); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(tokenPath)
	var tok Token
	if err := json.Unmarshal(b, &tok); err != nil || !reflect.DeepEqual(tok, result.Token) {
		t.Fatalf("token=%#v err=%v", tok, err)
	}
	env, _ := os.ReadFile(envPath)
	text := string(env)
	for _, line := range []string{"TWITCH_CLIENT_ID=publicclient1234567890", "TWITCH_CHANNEL=some_login", "TWITCH_BROADCASTER_ID=12345", "F11_QUEUE=Rongta_F11_Media", "TWITCH_TOKEN_FILE=" + tokenPath} {
		if !strings.Contains(text, line+"\n") {
			t.Errorf("missing %q in %s", line, text)
		}
	}
	if strings.Contains(text, "access-secret") || strings.Contains(text, "refresh-secret") || strings.Contains(text, "TWITCH_CLIENT_SECRET") {
		t.Fatalf("secret in environment: %s", text)
	}
	for _, p := range []string{tokenPath, envPath} {
		info, _ := os.Stat(p)
		if info.Mode().Perm() != 0600 {
			t.Errorf("%s mode=%o", p, info.Mode().Perm())
		}
	}
}

func TestValidateRequiresExactScopesAndClientIdentity(t *testing.T) {
	for _, scopes := range [][]string{{"bits:read"}, {"bits:read", "user:read:chat", "extra"}, {"user:read:chat", "bits:read"}} {
		err := validateIdentity(Identity{ClientID: "c", Login: "login", UserID: "1", Scopes: scopes}, "c")
		wantOK := len(scopes) == 2
		if (err == nil) != wantOK {
			t.Errorf("scopes=%v err=%v", scopes, err)
		}
	}
}

func TestCancellationIsHonored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(http.DefaultClient, "https://invalid.example").Start(ctx, "publicclient1234567890")
	if err == nil {
		t.Fatal("cancellation ignored")
	}
}
