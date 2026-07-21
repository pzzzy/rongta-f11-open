package twitchgift

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/twitchbanner"
)

func sampleCelebration() Celebration {
	return Celebration{CommunityID: "community-1", Total: 10, Gifter: "Gift Hero", Recipients: []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"}}
}

func TestGiftPrinterUsesStructuredArgumentsWithoutShell(t *testing.T) {
	var name string
	var args []string
	p := GiftPrinter{Binary: "/usr/local/bin/giftprint", Queue: "Rongta_F11_Media", Run: func(_ context.Context, n string, a ...string) ([]byte, error) {
		name, args = n, append([]string(nil), a...)
		return []byte(`{"ok":true,"submitted":true,"job_id":"Rongta_F11_Media-12"}`), nil
	}}
	r, err := p.Print(context.Background(), sampleCelebration())
	if err != nil || r.JobID != "Rongta_F11_Media-12" {
		t.Fatalf("r=%#v err=%v", r, err)
	}
	want := []string{"--queue", "Rongta_F11_Media", "--gifter", "Gift Hero", "--total", "10", "--missing", "0", "--recipients", "A,B,C,D,E,F,G,H,I,J"}
	if name != "/usr/local/bin/giftprint" || !reflect.DeepEqual(args, want) {
		t.Fatalf("name=%q args=%#v", name, args)
	}
}

func TestGiftProcessorReservesBeforePrintAndDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	j, err := twitchbanner.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	p := Processor{Journal: j, Printer: GiftPrinter{Binary: "giftprint", Queue: "Rongta_F11_Media", Run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		calls++
		data, err := twitchbanner.OpenJournal(path)
		if err != nil || data == nil {
			t.Fatalf("journal not durable before print: %v", err)
		}
		return []byte(`{"ok":true,"submitted":true,"job_id":"job-1"}`), nil
	}}}
	first, err := p.Process(context.Background(), sampleCelebration())
	if err != nil || !first.Submitted || calls != 1 {
		t.Fatalf("first=%#v calls=%d err=%v", first, calls, err)
	}
	second, err := p.Process(context.Background(), sampleCelebration())
	if err != nil || !second.Duplicate || calls != 1 {
		t.Fatalf("second=%#v calls=%d err=%v", second, calls, err)
	}
}

func TestParseOwnerOnlyGiftNotification(t *testing.T) {
	payload := map[string]any{"metadata": map[string]any{"message_type": "notification", "subscription_type": "channel.chat.message"}, "payload": map[string]any{"event": map[string]any{"broadcaster_user_id": "52588311", "chatter_user_id": "52588311", "message_id": "m1", "message": map[string]any{"text": "!testgift Hero | A,B,C,D,E,F,G,H,I,J"}}}}
	data, _ := json.Marshal(payload)
	c, ok, err := ParseTestNotification(data, "52588311")
	if err != nil || !ok || c.CommunityID != "testgift:m1" || len(c.Recipients) != 10 {
		t.Fatalf("c=%#v ok=%v err=%v", c, ok, err)
	}
	payload["payload"].(map[string]any)["event"].(map[string]any)["chatter_user_id"] = "other"
	data, _ = json.Marshal(payload)
	if _, ok, _ := ParseTestNotification(data, "52588311"); ok {
		t.Fatal("other accepted")
	}
}
