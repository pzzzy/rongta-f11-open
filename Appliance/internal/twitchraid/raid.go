package twitchraid

import (
	"encoding/json"
	"strings"
)

type Event struct {
	MessageID string
	Channel   string
	Viewers   int
}

func cleanName(raw string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune(" _.-'", r)) {
			return ""
		}
		if b.Len() < 25 {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func ParseNotification(data []byte, broadcasterID string) (Event, bool, error) {
	var m struct {
		Metadata struct {
			MessageID        string `json:"message_id"`
			MessageType      string `json:"message_type"`
			SubscriptionType string `json:"subscription_type"`
		} `json:"metadata"`
		Payload struct {
			Event struct {
				BroadcasterID string `json:"broadcaster_user_id"`
				FromID        string `json:"from_broadcaster_user_id"`
				FromLogin     string `json:"from_broadcaster_user_login"`
				FromName      string `json:"from_broadcaster_user_name"`
				Viewers       int    `json:"viewers"`
			} `json:"event"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return Event{}, false, err
	}
	e := m.Payload.Event
	if m.Metadata.MessageType != "notification" || m.Metadata.SubscriptionType != "channel.raid" || e.BroadcasterID != broadcasterID || e.FromID == "" || e.FromID == broadcasterID || e.Viewers <= 0 || m.Metadata.MessageID == "" {
		return Event{}, false, nil
	}
	channel := cleanName(e.FromName)
	if channel == "" {
		channel = cleanName(e.FromLogin)
	}
	if channel == "" {
		return Event{}, false, nil
	}
	return Event{MessageID: "raid:" + m.Metadata.MessageID, Channel: channel, Viewers: e.Viewers}, true, nil
}
