// Package twitchdevice implements Twitch's public-client device-code grant.
package twitchdevice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	OAuthBase        = "https://id.twitch.tv/oauth2"
	Scope            = "bits:read user:read:chat"
	MaxResponseBytes = 64 << 10
)

var (
	clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	loginPattern    = regexp.MustCompile(`^[a-z0-9_]{1,25}$`)
	userIDPattern   = regexp.MustCompile(`^[0-9]{1,20}$`)
	exactScopes     = []string{"bits:read", "user:read:chat"}
)

type State string

const (
	Pending    State = "authorization_pending"
	SlowDown   State = "slow_down"
	Authorized State = "authorized"
	Expired    State = "expired"
	Denied     State = "denied"
)

type PublicFlow struct {
	VerificationURI string    `json:"verification_uri"`
	UserCode        string    `json:"user_code"`
	ExpiresAt       time.Time `json:"expires_at"`
	Interval        int       `json:"interval"`
}

type Flow struct {
	ClientID        string
	DeviceCode      string
	VerificationURI string
	UserCode        string
	ExpiresAt       time.Time
	Interval        time.Duration
}

func (f Flow) Public() PublicFlow {
	return PublicFlow{VerificationURI: f.VerificationURI, UserCode: f.UserCode, ExpiresAt: f.ExpiresAt, Interval: int(f.Interval / time.Second)}
}

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Scope        []string  `json:"scope,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Identity struct {
	ClientID string   `json:"client_id"`
	Login    string   `json:"login"`
	UserID   string   `json:"user_id"`
	Scopes   []string `json:"scopes"`
}

type Result struct {
	Token    Token
	Identity Identity
}

type Client struct {
	http *http.Client
	base string
	now  func() time.Time
}

func New(h *http.Client, base string) *Client {
	if h == nil {
		h = &http.Client{Timeout: 15 * time.Second}
	}
	if base == "" {
		base = OAuthBase
	}
	return &Client{http: h, base: strings.TrimRight(base, "/"), now: time.Now}
}

func ValidateClientID(id string) error {
	if !clientIDPattern.MatchString(id) {
		return errors.New("invalid Twitch client ID")
	}
	return nil
}

func (c *Client) Start(ctx context.Context, clientID string) (Flow, error) {
	if err := ValidateClientID(clientID); err != nil {
		return Flow{}, err
	}
	var response struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := c.form(ctx, "/device", url.Values{"client_id": {clientID}, "scopes": {Scope}}, &response); err != nil {
		return Flow{}, err
	}
	uri, err := url.Parse(response.VerificationURI)
	if err != nil || uri.Scheme != "https" || uri.Host == "" || response.DeviceCode == "" || response.UserCode == "" || response.ExpiresIn < 1 || response.ExpiresIn > 1800 || response.Interval < 1 || response.Interval > 60 {
		return Flow{}, errors.New("invalid Twitch device response")
	}
	return Flow{ClientID: clientID, DeviceCode: response.DeviceCode, VerificationURI: response.VerificationURI, UserCode: response.UserCode, ExpiresAt: c.now().Add(time.Duration(response.ExpiresIn) * time.Second), Interval: time.Duration(response.Interval) * time.Second}, nil
}

func (c *Client) Poll(ctx context.Context, flow Flow) (Result, State, error) {
	if !flow.ExpiresAt.After(c.now()) {
		return Result{}, Expired, errors.New("Twitch authorization expired")
	}
	if err := ValidateClientID(flow.ClientID); err != nil || flow.DeviceCode == "" {
		return Result{}, Denied, errors.New("invalid device flow")
	}
	var response struct {
		AccessToken  string   `json:"access_token"`
		RefreshToken string   `json:"refresh_token"`
		ExpiresIn    int      `json:"expires_in"`
		Scope        []string `json:"scope"`
		TokenType    string   `json:"token_type"`
	}
	values := url.Values{"client_id": {flow.ClientID}, "device_code": {flow.DeviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "scopes": {Scope}}
	status, oauthMessage, err := c.formStatus(ctx, "/token", values, &response)
	if err != nil {
		return Result{}, Denied, err
	}
	if status != http.StatusOK {
		switch oauthMessage {
		case string(Pending):
			return Result{}, Pending, nil
		case string(SlowDown):
			return Result{}, SlowDown, nil
		case "expired_token":
			return Result{}, Expired, errors.New("Twitch authorization expired")
		case "access_denied":
			return Result{}, Denied, errors.New("Twitch authorization denied")
		default:
			return Result{}, Denied, errors.New("Twitch authorization failed")
		}
	}
	if response.AccessToken == "" || response.ExpiresIn < 1 || response.ExpiresIn > 86400 || !sameScopes(response.Scope) || !strings.EqualFold(response.TokenType, "bearer") {
		return Result{}, Denied, errors.New("invalid Twitch token response")
	}
	identity, err := c.validate(ctx, response.AccessToken, flow.ClientID)
	if err != nil {
		return Result{}, Denied, err
	}
	return Result{Token: Token{AccessToken: response.AccessToken, RefreshToken: response.RefreshToken, Scope: append([]string(nil), response.Scope...), TokenType: response.TokenType, ExpiresAt: c.now().Add(time.Duration(response.ExpiresIn) * time.Second).UTC()}, Identity: identity}, Authorized, nil
}

func (c *Client) validate(ctx context.Context, accessToken, clientID string) (Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/validate", nil)
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Authorization", "OAuth "+accessToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return Identity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		drain(resp.Body)
		return Identity{}, errors.New("Twitch token validation failed")
	}
	var identity Identity
	if err := decodeStrict(resp.Body, &identity); err != nil {
		return Identity{}, fmt.Errorf("invalid Twitch validation response: %w", err)
	}
	if err := validateIdentity(identity, clientID); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func validateIdentity(identity Identity, clientID string) error {
	if identity.ClientID != clientID || !loginPattern.MatchString(identity.Login) || !userIDPattern.MatchString(identity.UserID) || !sameScopes(identity.Scopes) {
		return errors.New("Twitch token identity or scopes did not match")
	}
	return nil
}

func sameScopes(scopes []string) bool {
	got := append([]string(nil), scopes...)
	sort.Strings(got)
	want := append([]string(nil), exactScopes...)
	sort.Strings(want)
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func (c *Client) form(ctx context.Context, path string, values url.Values, out any) error {
	status, _, err := c.formStatus(ctx, path, values, out)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return errors.New("Twitch request failed")
	}
	return nil
}

func (c *Client) formStatus(ctx context.Context, path string, values url.Values, out any) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, strings.NewReader(values.Encode()))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	data, err := readCapped(resp.Body)
	if err != nil {
		return 0, "", err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := decodeStrict(bytes.NewReader(data), out); err != nil {
			return 0, "", fmt.Errorf("invalid Twitch response: %w", err)
		}
		return resp.StatusCode, "", nil
	}
	var oauthErr struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	}
	if err := decodeStrict(bytes.NewReader(data), &oauthErr); err != nil {
		return resp.StatusCode, "", errors.New("Twitch request failed")
	}
	return resp.StatusCode, oauthErr.Message, nil
}

func readCapped(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxResponseBytes {
		return nil, errors.New("Twitch response too large")
	}
	return data, nil
}

func decodeStrict(r io.Reader, out any) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func drain(r io.Reader) { _, _ = io.Copy(io.Discard, io.LimitReader(r, MaxResponseBytes)) }

func Save(tokenPath, environmentPath string, result Result) error {
	if err := validateIdentity(result.Identity, result.Identity.ClientID); err != nil || result.Token.AccessToken == "" || !sameScopes(result.Token.Scope) {
		return errors.New("refusing to save invalid Twitch authorization")
	}
	token, err := json.Marshal(result.Token)
	if err != nil {
		return err
	}
	token = append(token, '\n')
	environment := fmt.Sprintf("TWITCH_CLIENT_ID=%s\nTWITCH_CHANNEL=%s\nTWITCH_BROADCASTER_ID=%s\nF11_QUEUE=Rongta_F11_Media\nTWITCH_TOKEN_FILE=%s\nTWITCH_JOURNAL_FILE=/var/lib/twitch-banner/events.jsonl\n", result.Identity.ClientID, result.Identity.Login, result.Identity.UserID, tokenPath)
	if err := atomicWrite(tokenPath, token); err != nil {
		return err
	}
	return atomicWrite(environmentPath, []byte(environment))
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".twitch-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	fail := func(e error) error { _ = file.Close(); return e }
	if err := file.Chmod(0600); err != nil {
		return fail(err)
	}
	if _, err := file.Write(data); err != nil {
		return fail(err)
	}
	if err := file.Sync(); err != nil {
		return fail(err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Public-client refresh tokens can be obtained by the device grant, but the
// existing twitch-banner daemon currently refreshes only confidential clients.
// The wizard therefore persists the refresh token without attempting refresh.
