package twitchbanner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	TwitchAPIBase   = "https://api.twitch.tv"
	TwitchOAuthBase = "https://id.twitch.tv/oauth2"
)

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Scope        []string  `json:"scope,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (t Token) NeedsRefresh(now time.Time) bool {
	return t.AccessToken == "" || t.ExpiresAt.IsZero() || !t.ExpiresAt.After(now.Add(5*time.Minute))
}

func AuthorizationURL(clientID, redirectURI string) (string, string, error) {
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", "", err
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	v := url.Values{"client_id": {clientID}, "redirect_uri": {redirectURI}, "response_type": {"code"}, "scope": {"bits:read"}, "state": {state}, "force_verify": {"true"}}
	return TwitchOAuthBase + "/authorize?" + v.Encode(), state, nil
}

type APIClient struct {
	ClientID    string
	AccessToken string
	BaseURL     string
	HTTP        *http.Client
}

type TokenIdentity struct {
	ClientID string   `json:"client_id"`
	Login    string   `json:"login"`
	Scopes   []string `json:"scopes"`
}

func (i TokenIdentity) Validate(clientID, login string) error {
	if i.ClientID != clientID {
		return errors.New("OAuth token belongs to a different Twitch application")
	}
	if !strings.EqualFold(i.Login, login) {
		return errors.New("OAuth token belongs to a different Twitch broadcaster")
	}
	for _, scope := range i.Scopes {
		if scope == "bits:read" {
			return nil
		}
	}
	return errors.New("OAuth token lacks bits:read")
}

func ValidateToken(ctx context.Context, h *http.Client, accessToken string) (TokenIdentity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, TwitchOAuthBase+"/validate", nil)
	if err != nil {
		return TokenIdentity{}, err
	}
	req.Header.Set("Authorization", "OAuth "+accessToken)
	if h == nil {
		h = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := h.Do(req)
	if err != nil {
		return TokenIdentity{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenIdentity{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return TokenIdentity{}, fmt.Errorf("Twitch token validation %s", resp.Status)
	}
	var identity TokenIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return TokenIdentity{}, err
	}
	return identity, nil
}

func (c APIClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	base := c.BaseURL
	if base == "" {
		base = TwitchAPIBase
	}
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Client-Id", c.ClientID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := h.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Twitch API %s: %s", resp.Status, string(data))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c APIClient) SubscribeCheer(ctx context.Context, broadcasterID, sessionID string) error {
	if broadcasterID == "" || sessionID == "" {
		return errors.New("broadcaster and WebSocket session IDs are required")
	}
	body := map[string]any{"type": "channel.cheer", "version": "1", "condition": map[string]string{"broadcaster_user_id": broadcasterID}, "transport": map[string]string{"method": "websocket", "session_id": sessionID}}
	return c.doJSON(ctx, http.MethodPost, "/helix/eventsub/subscriptions", body, nil)
}

func (c APIClient) UserByLogin(ctx context.Context, login string) (string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	path := "/helix/users?login=" + url.QueryEscape(login)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return "", err
	}
	if len(out.Data) != 1 {
		return "", errors.New("Twitch channel login not found")
	}
	return out.Data[0].ID, nil
}

func ExchangeCode(ctx context.Context, h *http.Client, clientID, secret, code, redirect string) (Token, error) {
	v := url.Values{"client_id": {clientID}, "client_secret": {secret}, "code": {code}, "grant_type": {"authorization_code"}, "redirect_uri": {redirect}}
	return tokenRequest(ctx, h, v)
}
func RefreshToken(ctx context.Context, h *http.Client, clientID, secret, refresh string) (Token, error) {
	v := url.Values{"client_id": {clientID}, "client_secret": {secret}, "grant_type": {"refresh_token"}, "refresh_token": {refresh}}
	return tokenRequest(ctx, h, v)
}
func tokenRequest(ctx context.Context, h *http.Client, v url.Values) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TwitchOAuthBase+"/token", strings.NewReader(v.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if h == nil {
		h = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := h.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Token{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Token{}, fmt.Errorf("Twitch OAuth %s: %s", resp.Status, string(data))
	}
	var wire struct {
		AccessToken  string   `json:"access_token"`
		RefreshToken string   `json:"refresh_token"`
		ExpiresIn    int      `json:"expires_in"`
		Scope        []string `json:"scope"`
		TokenType    string   `json:"token_type"`
	}
	if err = json.Unmarshal(data, &wire); err != nil {
		return Token{}, err
	}
	if wire.AccessToken == "" || wire.RefreshToken == "" {
		return Token{}, errors.New("Twitch OAuth response omitted token")
	}
	return Token{AccessToken: wire.AccessToken, RefreshToken: wire.RefreshToken, Scope: wire.Scope, TokenType: wire.TokenType, ExpiresAt: time.Now().Add(time.Duration(wire.ExpiresIn) * time.Second)}, nil
}
