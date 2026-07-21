package twitchraid

import (
	"encoding/json"
	"testing"
)

func raidNotification(event map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{"message_id": "delivery-raid-1", "message_type": "notification", "subscription_type": "channel.raid"},
		"payload":  map[string]any{"event": event},
	})
	return b
}

func TestParseIncomingRaid(t *testing.T) {
	e, ok, err := ParseNotification(raidNotification(map[string]any{
		"broadcaster_user_id":         "52588311",
		"from_broadcaster_user_id":    "12345",
		"from_broadcaster_user_login": "raid_channel",
		"from_broadcaster_user_name":  "Raid Channel",
		"viewers":                     47,
	}), "52588311")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if e.MessageID != "raid:delivery-raid-1" || e.Channel != "Raid Channel" || e.Viewers != 47 {
		t.Fatalf("event=%#v", e)
	}
}

func TestParseRaidRejectsOutgoingWrongTargetAndInvalidCounts(t *testing.T) {
	cases := []map[string]any{
		{"broadcaster_user_id": "other", "from_broadcaster_user_name": "A", "viewers": 10},
		{"broadcaster_user_id": "52588311", "from_broadcaster_user_id": "52588311", "from_broadcaster_user_name": "A", "viewers": 10},
		{"broadcaster_user_id": "52588311", "from_broadcaster_user_name": "A", "viewers": 0},
		{"broadcaster_user_id": "52588311", "from_broadcaster_user_name": "A", "viewers": -1},
	}
	for _, event := range cases {
		if _, ok, err := ParseNotification(raidNotification(event), "52588311"); err != nil || ok {
			t.Fatalf("accepted event=%#v ok=%v err=%v", event, ok, err)
		}
	}
}

func TestParseRaidFallsBackToLoginForUnsupportedName(t *testing.T) {
	e, ok, err := ParseNotification(raidNotification(map[string]any{
		"broadcaster_user_id": "52588311", "from_broadcaster_user_id": "123",
		"from_broadcaster_user_login": "ascii_login", "from_broadcaster_user_name": "Raid💚", "viewers": 10,
	}), "52588311")
	if err != nil || !ok || e.Channel != "ascii_login" {
		t.Fatalf("event=%#v ok=%v err=%v", e, ok, err)
	}
}

func TestParseRaidDoesNotAcceptMalformedMetadata(t *testing.T) {
	if _, ok, err := ParseNotification([]byte(`{"metadata":{"message_type":"notification","subscription_type":"channel.cheer"}}`), "52588311"); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
