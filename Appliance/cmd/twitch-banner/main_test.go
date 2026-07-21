package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/twitchgift"
)

func TestWriteReadyMarkerIsAtomicAndNonsecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ready.json")
	if err := writeReadyMarker(path, "12345"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{"12345", "channel.cheer", "channel.chat.message", "channel.chat.notification", "channel.raid"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	if strings.Contains(strings.ToLower(text), "token") {
		t.Fatalf("secret-like field in marker: %s", text)
	}
	if mode, _ := os.Stat(path); mode.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", mode.Mode().Perm())
	}
}

func TestEnvConfigAllowsPublicClientWithoutSecret(t *testing.T) {
	t.Setenv("TWITCH_CLIENT_ID", "publicclient123")
	t.Setenv("TWITCH_CLIENT_SECRET", "")
	t.Setenv("TWITCH_CHANNEL", "channel")
	t.Setenv("TWITCH_BROADCASTER_ID", "123")
	if _, err := envConfig(); err != nil {
		t.Fatal(err)
	}
}

func websocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverConn := make(chan *websocket.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConn <- c
	}))
	url := "ws" + strings.TrimPrefix(s.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	server := <-serverConn
	cleanup := func() { _ = client.Close(); _ = server.Close(); s.Close() }
	return client, server, cleanup
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("socket reader goroutine did not stop")
	}
}

func TestSocketReaderStopJoinsBlockedRead(t *testing.T) {
	client, _, cleanup := websocketPair(t)
	defer cleanup()
	r := startSocketReader(context.Background(), client)
	r.stop()
	waitDone(t, r.done)
}

func TestSocketReaderStopIsIdempotent(t *testing.T) {
	client, _, cleanup := websocketPair(t)
	defer cleanup()
	r := startSocketReader(context.Background(), client)
	r.stop()
	r.stop()
	waitDone(t, r.done)
}

func TestSocketReaderStopJoinsWhenUnreadMessageIsWaiting(t *testing.T) {
	client, server, cleanup := websocketPair(t)
	defer cleanup()
	r := startSocketReader(context.Background(), client)
	if err := server.WriteMessage(websocket.TextMessage, []byte("one")); err != nil {
		t.Fatal(err)
	}
	// The unbuffered delivery blocks until read or cancellation. stop must release it.
	time.Sleep(20 * time.Millisecond)
	r.stop()
	waitDone(t, r.done)
}

func TestSocketReaderHandoffStopsBothReaders(t *testing.T) {
	firstClient, _, firstCleanup := websocketPair(t)
	defer firstCleanup()
	secondClient, _, secondCleanup := websocketPair(t)
	defer secondCleanup()
	first := startSocketReader(context.Background(), firstClient)
	second := startSocketReader(context.Background(), secondClient)
	first.stop()
	waitDone(t, first.done)
	second.stop()
	waitDone(t, second.done)
}

func TestCollectorSurvivesOrdinarySocketFailureAndFlushes(t *testing.T) {
	collector := twitchgift.NewCollector(10 * time.Millisecond)
	now := time.Now()
	collector.Accept(twitchgift.Event{Kind: twitchgift.KindStart, CommunityID: "gift", Total: 10, Gifter: "Hero"}, now)
	client, server, cleanup := websocketPair(t)
	defer cleanup()
	r := startSocketReader(context.Background(), client)
	_ = server.Close()
	select {
	case read := <-r.reads:
		if read.err == nil {
			t.Fatal("expected ordinary socket failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("socket failure not observed")
	}
	r.stop()
	got := collector.FlushDue(now.Add(20 * time.Millisecond))
	if len(got) != 1 || got[0].CommunityID != "gift" || got[0].Missing != 10 {
		t.Fatalf("pending gift lost across failure: %#v", got)
	}
}

func TestDisconnectedGiftFlushIsFIFO(t *testing.T) {
	collector := twitchgift.NewCollector(time.Millisecond)
	now := time.Now()
	collector.Accept(twitchgift.Event{Kind: twitchgift.KindStart, CommunityID: "first", Total: 10, Gifter: "A"}, now)
	collector.Accept(twitchgift.Event{Kind: twitchgift.KindStart, CommunityID: "second", Total: 10, Gifter: "B"}, now.Add(time.Microsecond))
	var mu sync.Mutex
	var order []string
	for _, gift := range collector.FlushDue(now.Add(time.Second)) {
		mu.Lock()
		order = append(order, gift.CommunityID)
		mu.Unlock()
	}
	if strings.Join(order, ",") != "first,second" {
		t.Fatalf("order=%v", order)
	}
}

func TestParentCancellationStopsReader(t *testing.T) {
	client, _, cleanup := websocketPair(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	r := startSocketReader(ctx, client)
	cancel()
	r.stop()
	waitDone(t, r.done)
}
