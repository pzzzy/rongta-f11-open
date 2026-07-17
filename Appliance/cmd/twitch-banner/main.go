package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/twitchbanner"
)

const redirectURI = "http://localhost:17563/twitch/callback"

var wsURL = "wss://eventsub.wss.twitch.tv/ws?keepalive_timeout_seconds=30"

type config struct {
	ClientID, ClientSecret, Channel, BroadcasterID, Queue, TokenFile, JournalFile string
}

func envConfig() (config, error) {
	c := config{ClientID: os.Getenv("TWITCH_CLIENT_ID"), ClientSecret: os.Getenv("TWITCH_CLIENT_SECRET"), Channel: strings.ToLower(os.Getenv("TWITCH_CHANNEL")), BroadcasterID: os.Getenv("TWITCH_BROADCASTER_ID"), Queue: os.Getenv("F11_QUEUE"), TokenFile: os.Getenv("TWITCH_TOKEN_FILE"), JournalFile: os.Getenv("TWITCH_JOURNAL_FILE")}
	if c.TokenFile == "" {
		c.TokenFile = "/var/lib/twitch-banner/token.json"
	}
	if c.JournalFile == "" {
		c.JournalFile = "/var/lib/twitch-banner/events.jsonl"
	}
	if c.Queue == "" {
		c.Queue = "Rongta_F11_Media"
	}
	if c.ClientID == "" || c.ClientSecret == "" || c.Channel == "" || c.BroadcasterID == "" {
		return c, errors.New("TWITCH_CLIENT_ID, TWITCH_CLIENT_SECRET, TWITCH_CHANNEL, and TWITCH_BROADCASTER_ID are required")
	}
	return c, nil
}

func saveToken(path string, t twitchbanner.Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func loadToken(path string) (t twitchbanner.Token, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return t, err
	}
	err = json.Unmarshal(b, &t)
	return
}

func authorize(ctx context.Context, c config) error {
	authURL, state, err := twitchbanner.AuthorizationURL(c.ClientID, redirectURI)
	if err != nil {
		return err
	}
	results := make(chan twitchbanner.OAuthCallback, 1)
	mux := http.NewServeMux()
	mux.Handle("/twitch/callback", twitchbanner.OAuthCallbackHandler(state, results))
	srv := &http.Server{Addr: "127.0.0.1:17563", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		if e := srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			errCh <- e
		}
	}()
	fmt.Println("Open this URL in your browser:")
	fmt.Println(authURL)
	fmt.Println("Waiting for Twitch callback on", redirectURI)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	case cb := <-results:
		_ = srv.Shutdown(context.Background())
		tok, err := twitchbanner.ExchangeCode(ctx, nil, c.ClientID, c.ClientSecret, cb.Code, redirectURI)
		if err != nil {
			return err
		}
		identity, err := twitchbanner.ValidateToken(ctx, nil, tok.AccessToken)
		if err != nil {
			return err
		}
		if err = identity.Validate(c.ClientID, c.Channel, c.BroadcasterID); err != nil {
			return err
		}
		if err = saveToken(c.TokenFile, tok); err != nil {
			return err
		}
		fmt.Println("Authorization saved successfully.")
		return nil
	}
}

func freshToken(ctx context.Context, c config) (twitchbanner.Token, error) {
	t, err := loadToken(c.TokenFile)
	if err != nil {
		return t, err
	}
	if t.NeedsRefresh(time.Now()) {
		t, err = twitchbanner.RefreshToken(ctx, nil, c.ClientID, c.ClientSecret, t.RefreshToken)
		if err != nil {
			return t, err
		}
		if err = saveToken(c.TokenFile, t); err != nil {
			return t, err
		}
	}
	identity, err := twitchbanner.ValidateToken(ctx, nil, t.AccessToken)
	if err != nil {
		return t, err
	}
	if err = identity.Validate(c.ClientID, c.Channel, c.BroadcasterID); err != nil {
		return t, err
	}
	return t, nil
}

type welcome struct {
	Metadata struct {
		MessageType string `json:"message_type"`
	} `json:"metadata"`
	Payload struct {
		Session struct {
			ID           string `json:"id"`
			ReconnectURL string `json:"reconnect_url"`
		} `json:"session"`
	} `json:"payload"`
}

func dialWelcome(ctx context.Context, target string) (*websocket.Conn, welcome, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, target, nil)
	if err != nil {
		return nil, welcome{}, err
	}
	conn.SetReadLimit(1 << 20)
	if err = conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		conn.Close()
		return nil, welcome{}, err
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return nil, welcome{}, err
	}
	var w welcome
	if err = json.Unmarshal(data, &w); err != nil || w.Metadata.MessageType != "session_welcome" || w.Payload.Session.ID == "" {
		conn.Close()
		return nil, welcome{}, errors.New("Twitch did not send a valid EventSub welcome")
	}
	return conn, w, nil
}

func runConnection(ctx context.Context, c config, p twitchbanner.Processor) error {
	tok, err := freshToken(ctx, c)
	if err != nil {
		return err
	}
	api := twitchbanner.APIClient{ClientID: c.ClientID, AccessToken: tok.AccessToken}
	broadcaster, err := api.UserByLogin(ctx, c.Channel)
	if err != nil {
		return err
	}
	if broadcaster != c.BroadcasterID {
		return errors.New("configured Twitch login no longer resolves to pinned broadcaster ID")
	}
	broadcaster = c.BroadcasterID
	conn, w, err := dialWelcome(ctx, wsURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err = api.SubscribeCheer(ctx, broadcaster, w.Payload.Session.ID); err != nil {
		return err
	}
	if err = api.SubscribeChat(ctx, broadcaster, w.Payload.Session.ID); err != nil {
		return err
	}
	log.Printf("subscribed channel=%s broadcaster_id=%s events=channel.cheer,channel.chat.message", c.Channel, broadcaster)
	for {
		if err = conn.SetReadDeadline(time.Now().Add(45 * time.Second)); err != nil {
			return err
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var meta struct {
			Metadata struct {
				MessageType string `json:"message_type"`
			} `json:"metadata"`
			Payload struct {
				Session struct {
					ReconnectURL string `json:"reconnect_url"`
				} `json:"session"`
			} `json:"payload"`
		}
		_ = json.Unmarshal(data, &meta)
		switch meta.Metadata.MessageType {
		case "session_keepalive":
			continue
		case "session_reconnect":
			reconnectURL := meta.Payload.Session.ReconnectURL
			if reconnectURL == "" {
				return errors.New("Twitch reconnect omitted URL")
			}
			newConn, _, handoffErr := dialWelcome(ctx, reconnectURL)
			if handoffErr != nil {
				return fmt.Errorf("Twitch reconnect handoff failed: %w", handoffErr)
			}
			oldConn := conn
			conn = newConn
			_ = oldConn.Close()
			log.Printf("EventSub reconnect handoff complete")
			continue
		case "revocation":
			return errors.New("Twitch revoked EventSub subscription")
		}
		env, ok, parseErr := twitchbanner.ParseNotification(data)
		if parseErr != nil {
			log.Printf("invalid notification: %v", parseErr)
			continue
		}
		if !ok {
			env, ok, parseErr = twitchbanner.ParseChatCommand(data, broadcaster)
			if parseErr != nil {
				log.Printf("invalid chat notification: %v", parseErr)
				continue
			}
			if !ok {
				continue
			}
		}
		result, processErr := p.Process(ctx, env)
		if processErr != nil {
			log.Printf("event=%s error=%v", env.MessageID, processErr)
			continue
		}
		if result.Duplicate {
			log.Printf("event=%s duplicate ignored", env.MessageID)
		} else if result.Submitted {
			log.Printf("event=%s bits=%d job=%s text=%q", env.MessageID, env.Cheer.Bits, result.JobID, result.Text)
		} else {
			log.Printf("event=%s bits=%d below threshold", env.MessageID, env.Cheer.Bits)
		}
	}
}

func run(ctx context.Context, c config) error {
	j, err := twitchbanner.OpenJournal(c.JournalFile)
	if err != nil {
		return err
	}
	p := twitchbanner.Processor{Journal: j, Printer: twitchbanner.BannerPrinter{Binary: "/usr/local/bin/bannerprint", Queue: c.Queue}}
	backoff := time.Second
	for {
		if err := runConnection(ctx, c, p); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("connection ended: %v; reconnecting in %s", err, backoff)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < time.Minute {
			backoff *= 2
		}
		var b [1]byte
		_, _ = rand.Read(b[:])
		time.Sleep(time.Duration(b[0]%4) * time.Second)
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if len(os.Args) != 2 || (os.Args[1] != "authorize" && os.Args[1] != "run") {
		fmt.Fprintln(os.Stderr, "usage: twitch-banner authorize|run")
		os.Exit(2)
	}
	c, err := envConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if os.Args[1] == "authorize" {
		err = authorize(ctx, c)
	} else {
		err = run(ctx, c)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
