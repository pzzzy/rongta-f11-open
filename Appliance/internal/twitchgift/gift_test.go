package twitchgift

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func notification(notice string, event map[string]any) []byte {
	event["broadcaster_user_id"] = "52588311"
	event["notice_type"] = notice
	b, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{"message_type": "notification", "subscription_type": "channel.chat.notification"},
		"payload":  map[string]any{"event": event},
	})
	return b
}

func TestParseCommunityGiftAndRecipients(t *testing.T) {
	start, ok, err := ParseNotification(notification("community_sub_gift", map[string]any{
		"message_id": "community-msg", "chatter_user_id": "gifter-id", "chatter_user_name": "Generous One", "chatter_is_anonymous": false,
		"community_sub_gift": map[string]any{"id": "community-123", "total": 10, "sub_tier": "1000"},
	}), "52588311")
	if err != nil || !ok || start.Kind != KindStart || start.CommunityID != "community-123" || start.Total != 10 || start.Gifter != "Generous One" {
		t.Fatalf("start=%#v ok=%v err=%v", start, ok, err)
	}
	recipient, ok, err := ParseNotification(notification("sub_gift", map[string]any{
		"message_id": "recipient-msg", "sub_gift": map[string]any{"community_gift_id": "community-123", "recipient_user_id": "recipient-1", "recipient_user_name": "New Friend"},
	}), "52588311")
	if err != nil || !ok || recipient.Kind != KindRecipient || recipient.Recipient != "New Friend" || recipient.RecipientID != "recipient-1" || recipient.CommunityID != "community-123" {
		t.Fatalf("recipient=%#v ok=%v err=%v", recipient, ok, err)
	}
}

func TestUnsupportedDisplayNamesFallBackToTwitchLogins(t *testing.T) {
	for _, display := range []string{"贈り主💚", "Alice💚", strings.Repeat("A", 48) + "💚"} {
		start, ok, err := ParseNotification(notification("community_sub_gift", map[string]any{
			"chatter_user_name": display, "chatter_user_login": "gift_login",
			"community_sub_gift": map[string]any{"id": "community-unicode", "total": 10},
		}), "52588311")
		if err != nil || !ok || start.Gifter != "gift_login" {
			t.Fatalf("display=%q start=%#v ok=%v err=%v", display, start, ok, err)
		}
		recipient, ok, err := ParseNotification(notification("sub_gift", map[string]any{
			"sub_gift": map[string]any{"community_gift_id": "community-unicode", "recipient_user_id": "r1", "recipient_user_name": display, "recipient_user_login": "recipient_login"},
		}), "52588311")
		if err != nil || !ok || recipient.Recipient != "recipient_login" {
			t.Fatalf("display=%q recipient=%#v ok=%v err=%v", display, recipient, ok, err)
		}
	}
}

func TestParseRejectsWrongBroadcasterAndUnderTen(t *testing.T) {
	data := notification("community_sub_gift", map[string]any{"community_sub_gift": map[string]any{"id": "c", "total": 9}})
	if _, ok, err := ParseNotification(data, "52588311"); err != nil || ok {
		t.Fatalf("9 accepted: %v %v", ok, err)
	}
	var payload map[string]any
	_ = json.Unmarshal(notification("community_sub_gift", map[string]any{"community_sub_gift": map[string]any{"id": "c", "total": 10}}), &payload)
	payload["payload"].(map[string]any)["event"].(map[string]any)["broadcaster_user_id"] = "wrong"
	data, _ = json.Marshal(payload)
	if _, ok, err := ParseNotification(data, "52588311"); err != nil || ok {
		t.Fatalf("wrong broadcaster accepted: %v %v", ok, err)
	}
}

func TestOwnerOnlyTestCommand(t *testing.T) {
	text := "!testgift Big Gifter | Alice, Bob, Carol, Dave, Eve, Frank, Grace, Heidi, Ivan, Judy"
	cmd, ok, err := ParseTestCommand("chat-1", "52588311", "52588311", text)
	if err != nil || !ok || cmd.CommunityID != "testgift:chat-1" || cmd.Gifter != "Big Gifter" || len(cmd.Recipients) != 10 {
		t.Fatalf("cmd=%#v ok=%v err=%v", cmd, ok, err)
	}
	if _, ok, _ := ParseTestCommand("chat-2", "other", "52588311", text); ok {
		t.Fatal("other user accepted")
	}
	if _, ok, _ := ParseTestCommand("chat-3", "52588311", "52588311", "!testgift G | A,B"); ok {
		t.Fatal("under-10 test accepted")
	}
}

func TestCollectorCompletesAndDeduplicatesRecipients(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewCollector(12 * time.Second)
	if ready := c.Accept(Event{Kind: KindStart, CommunityID: "c1", Total: 3, Gifter: "G"}, now); len(ready) != 0 {
		t.Fatal(ready)
	}
	for i, name := range []string{"A", "A", "B"} {
		id := []string{"same", "same", "b"}[i]
		if ready := c.Accept(Event{Kind: KindRecipient, CommunityID: "c1", Recipient: name, RecipientID: id}, now); len(ready) != 0 {
			t.Fatal(ready)
		}
	}
	ready := c.Accept(Event{Kind: KindRecipient, CommunityID: "c1", Recipient: "C", RecipientID: "c"}, now)
	if len(ready) != 1 || len(ready[0].Recipients) != 3 || ready[0].Missing != 0 {
		t.Fatalf("ready=%#v", ready)
	}
}

func TestCollectorTimeoutShowsMissingAndRecipientBeforeStart(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewCollector(12 * time.Second)
	c.Accept(Event{Kind: KindRecipient, CommunityID: "c2", Recipient: "Early", RecipientID: "early"}, now)
	c.Accept(Event{Kind: KindStart, CommunityID: "c2", Total: 10, Gifter: "G"}, now)
	c.Accept(Event{Kind: KindRecipient, CommunityID: "c2", Recipient: "Later", RecipientID: "later"}, now)
	if got := c.FlushDue(now.Add(11 * time.Second)); len(got) != 0 {
		t.Fatal(got)
	}
	got := c.FlushDue(now.Add(12 * time.Second))
	if len(got) != 1 || len(got[0].Recipients) != 2 || got[0].Missing != 8 {
		t.Fatalf("got=%#v", got)
	}
}

func TestCollectorFlushesDueCelebrationsInArrivalOrder(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewCollector(time.Second)
	c.Accept(Event{Kind: KindStart, CommunityID: "first", Total: 10, Gifter: "A"}, now)
	c.Accept(Event{Kind: KindStart, CommunityID: "second", Total: 10, Gifter: "B"}, now.Add(time.Millisecond))
	got := c.FlushDue(now.Add(2 * time.Second))
	if len(got) != 2 || got[0].CommunityID != "first" || got[1].CommunityID != "second" {
		t.Fatalf("order=%#v", got)
	}
}

func TestDuplicateAggregateDoesNotExtendCollectionDeadline(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewCollector(12 * time.Second)
	start := Event{Kind: KindStart, CommunityID: "gift", Total: 10, Gifter: "A"}
	c.Accept(start, now)
	c.Accept(start, now.Add(11*time.Second))
	got := c.FlushDue(now.Add(12 * time.Second))
	if len(got) != 1 || got[0].CommunityID != "gift" {
		t.Fatalf("deadline extended by duplicate: %#v", got)
	}
}

func TestCollectorIgnoresDuplicateCompletedCommunity(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewCollector(time.Second)
	c.Accept(Event{Kind: KindStart, CommunityID: "c3", Total: 1, Gifter: "G"}, now)
	if len(c.Accept(Event{Kind: KindRecipient, CommunityID: "c3", Recipient: "A", RecipientID: "a"}, now)) != 1 {
		t.Fatal("not ready")
	}
	if got := c.Accept(Event{Kind: KindStart, CommunityID: "c3", Total: 1, Gifter: "G"}, now); len(got) != 0 {
		t.Fatalf("duplicate=%#v", got)
	}
}
