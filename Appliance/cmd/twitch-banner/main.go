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
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/twitchbanner"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/twitchgift"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/twitchraid"
)

const redirectURI = "http://localhost:17563/twitch/callback"

var wsURL = "wss://eventsub.wss.twitch.tv/ws?keepalive_timeout_seconds=30"

type config struct {
	ClientID, ClientSecret, Channel, BroadcasterID, Queue, TokenFile, JournalFile, ReadyFile string
}

func envConfig() (config, error) {
	c := config{ClientID: os.Getenv("TWITCH_CLIENT_ID"), ClientSecret: os.Getenv("TWITCH_CLIENT_SECRET"), Channel: strings.ToLower(os.Getenv("TWITCH_CHANNEL")), BroadcasterID: os.Getenv("TWITCH_BROADCASTER_ID"), Queue: os.Getenv("F11_QUEUE"), TokenFile: os.Getenv("TWITCH_TOKEN_FILE"), JournalFile: os.Getenv("TWITCH_JOURNAL_FILE"), ReadyFile: os.Getenv("TWITCH_READY_FILE")}
	if c.TokenFile == "" {
		c.TokenFile = "/var/lib/twitch-banner/token.json"
	}
	if c.JournalFile == "" {
		c.JournalFile = "/var/lib/twitch-banner/events.jsonl"
	}
	if c.ReadyFile == "" {
		c.ReadyFile = "/var/lib/twitch-banner/eventsub-ready.json"
	}
	if c.Queue == "" {
		c.Queue = "Rongta_F11_Media"
	}
	if c.ClientID == "" || c.Channel == "" || c.BroadcasterID == "" {
		return c, errors.New("TWITCH_CLIENT_ID, TWITCH_CHANNEL, and TWITCH_BROADCASTER_ID are required")
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

var requiredSubscriptions = []string{"channel.cheer", "channel.chat.message", "channel.chat.notification", "channel.raid"}

func writeReadyMarker(path, broadcaster string) error {
	marker := struct {
		BroadcasterID string   `json:"broadcaster_id"`
		Subscriptions []string `json:"subscriptions"`
		ReadyAt       string   `json:"ready_at"`
	}{broadcaster, requiredSubscriptions, time.Now().UTC().Format(time.RFC3339)}
	b, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".new"
	if err = os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func authorize(ctx context.Context, c config) error {
	if c.ClientSecret == "" {
		return errors.New("TWITCH_CLIENT_SECRET is required for authorization-code setup; use the image wizard for public device authorization")
	}
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
	refreshed := false
	if t.NeedsRefresh(time.Now()) {
		t, err = twitchbanner.RefreshToken(ctx, nil, c.ClientID, c.ClientSecret, t.RefreshToken)
		if err != nil {
			return t, err
		}
		refreshed = true
	}
	identity, err := twitchbanner.ValidateToken(ctx, nil, t.AccessToken)
	if err != nil {
		return t, err
	}
	if err = identity.Validate(c.ClientID, c.Channel, c.BroadcasterID); err != nil {
		return t, err
	}
	if refreshed {
		if err = saveToken(c.TokenFile, t); err != nil {
			return t, err
		}
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

func processGift(ctx context.Context, p twitchgift.Processor, gift twitchgift.Celebration) {
	result, err := p.Process(ctx, gift)
	if err != nil {
		log.Printf("gift=%s error=%v", gift.CommunityID, err)
		return
	}
	if result.Duplicate {
		log.Printf("gift=%s duplicate ignored", gift.CommunityID)
	} else if result.Submitted {
		log.Printf("gift=%s total=%d named=%d missing=%d job=%s", gift.CommunityID, gift.Total, len(gift.Recipients), gift.Missing, result.JobID)
	}
}

type socketRead struct {
	data []byte
	err  error
}

type socketReader struct {
	conn   *websocket.Conn
	reads  <-chan socketRead
	cancel context.CancelFunc
	done   <-chan struct{}
	once   sync.Once
}

func startSocketReader(parent context.Context, conn *websocket.Conn) *socketReader {
	ctx, cancel := context.WithCancel(parent)
	reads := make(chan socketRead)
	done := make(chan struct{})
	r := &socketReader{conn: conn, reads: reads, cancel: cancel, done: done}
	go func() {
		defer close(done)
		defer close(reads)
		for {
			if err := conn.SetReadDeadline(time.Now().Add(45 * time.Second)); err != nil {
				select {
				case reads <- socketRead{err: err}:
				case <-ctx.Done():
				}
				return
			}
			_, data, err := conn.ReadMessage()
			select {
			case reads <- socketRead{data: data, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return r
}

func (r *socketReader) stop() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		r.cancel()
		_ = r.conn.Close()
		<-r.done
	})
}

func flushGifts(ctx context.Context, collector *twitchgift.Collector, gifts twitchgift.Processor, now time.Time) {
	for _, gift := range collector.FlushDue(now) {
		processGift(ctx, gifts, gift)
	}
}

func runConnection(ctx context.Context, c config, p twitchbanner.Processor, gifts twitchgift.Processor, raids twitchraid.Processor, collector *twitchgift.Collector) error {
	if err := os.Remove(c.ReadyFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
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
	if err = api.SubscribeCheer(ctx, broadcaster, w.Payload.Session.ID); err != nil {
		_ = conn.Close()
		return err
	}
	if err = api.SubscribeChat(ctx, broadcaster, w.Payload.Session.ID); err != nil {
		_ = conn.Close()
		return err
	}
	if err = api.SubscribeChatNotifications(ctx, broadcaster, w.Payload.Session.ID); err != nil {
		_ = conn.Close()
		return err
	}
	if err = api.SubscribeRaid(ctx, broadcaster, w.Payload.Session.ID); err != nil {
		_ = conn.Close()
		return err
	}
	if err = writeReadyMarker(c.ReadyFile, broadcaster); err != nil {
		_ = conn.Close()
		return err
	}
	log.Printf("subscribed channel=%s broadcaster_id=%s events=channel.cheer,channel.chat.message,channel.chat.notification,channel.raid", c.Channel, broadcaster)
	reader := startSocketReader(ctx, conn)
	defer func() { reader.stop() }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var data []byte
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			for _, gift := range collector.FlushDue(now) {
				processGift(ctx, gifts, gift)
			}
			continue
		case read, open := <-reader.reads:
			if !open {
				return errors.New("Twitch socket reader closed")
			}
			if read.err != nil {
				return read.err
			}
			data = read.data
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
			oldReader := reader
			conn = newConn
			reader = startSocketReader(ctx, conn)
			oldReader.stop()
			log.Printf("EventSub reconnect handoff complete")
			continue
		case "revocation":
			return errors.New("Twitch revoked EventSub subscription")
		}
		raidEvent, raidOK, raidErr := twitchraid.ParseNotification(data, broadcaster)
		if raidErr != nil {
			log.Printf("invalid raid notification: %v", raidErr)
			continue
		}
		if raidOK {
			result, processErr := raids.Process(ctx, raidEvent)
			if processErr != nil {
				log.Printf("event=%s error=%v", raidEvent.MessageID, processErr)
			} else if result.Duplicate {
				log.Printf("event=%s duplicate raid ignored", raidEvent.MessageID)
			} else if result.Submitted {
				log.Printf("event=%s raid channel=%q viewers=%d job=%s", raidEvent.MessageID, raidEvent.Channel, raidEvent.Viewers, result.JobID)
			}
			continue
		}
		giftEvent, giftOK, giftErr := twitchgift.ParseNotification(data, broadcaster)
		if giftErr != nil {
			log.Printf("invalid gift notification: %v", giftErr)
			continue
		}
		if giftOK {
			for _, gift := range collector.Accept(giftEvent, time.Now()) {
				processGift(ctx, gifts, gift)
			}
			continue
		}
		giftTest, testOK, testErr := twitchgift.ParseTestNotification(data, broadcaster)
		if testErr != nil {
			log.Printf("invalid gift test command: %v", testErr)
			continue
		}
		if testOK {
			processGift(ctx, gifts, giftTest)
			continue
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
	if info, err := os.Stat("/usr/local/bin/bannerprint"); err != nil || info.Mode()&0111 == 0 {
		return errors.New("bannerprint is missing or not executable")
	}
	if info, err := os.Stat("/usr/local/bin/giftprint"); err != nil || info.Mode()&0111 == 0 {
		return errors.New("giftprint is missing or not executable")
	}
	if info, err := os.Stat("/usr/local/bin/raidprint"); err != nil || info.Mode()&0111 == 0 {
		return errors.New("raidprint is missing or not executable")
	}
	j, err := twitchbanner.OpenJournal(c.JournalFile)
	if err != nil {
		return err
	}
	p := twitchbanner.Processor{Journal: j, Printer: twitchbanner.BannerPrinter{Binary: "/usr/local/bin/bannerprint", Queue: c.Queue}}
	gifts := twitchgift.Processor{Journal: j, Printer: twitchgift.GiftPrinter{Binary: "/usr/local/bin/giftprint", Queue: c.Queue}}
	raids := twitchraid.Processor{Journal: j, Printer: twitchraid.Printer{Binary: "/usr/local/bin/raidprint", Queue: c.Queue}}
	collector := twitchgift.NewCollector(12 * time.Second)
	backoff := time.Second
	for {
		if err := runConnection(ctx, c, p, gifts, raids, collector); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("connection ended: %v; reconnecting in %s", err, backoff)
		}
		jitter := time.Duration(0)
		var b [1]byte
		if _, err := rand.Read(b[:]); err == nil {
			jitter = time.Duration(b[0]%4) * time.Second
		}
		wait := backoff + jitter
		ticker := time.NewTicker(250 * time.Millisecond)
		timer := time.NewTimer(wait)
		waiting := true
		for waiting {
			select {
			case <-ctx.Done():
				ticker.Stop()
				timer.Stop()
				return ctx.Err()
			case now := <-ticker.C:
				flushGifts(ctx, collector, gifts, now)
			case <-timer.C:
				waiting = false
			}
		}
		ticker.Stop()
		if backoff < time.Minute {
			backoff *= 2
		}
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
