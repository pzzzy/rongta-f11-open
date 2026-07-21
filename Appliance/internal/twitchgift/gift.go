package twitchgift

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Kind int

const (
	KindUnknown Kind = iota
	KindStart
	KindRecipient
)

type Event struct {
	Kind        Kind
	CommunityID string
	Total       int
	Gifter      string
	Recipient   string
	RecipientID string
	Anonymous   bool
	Recipients  []string
}

type Celebration struct {
	CommunityID string
	Total       int
	Gifter      string
	Recipients  []string
	Missing     int
	Test        bool
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
			MessageType      string `json:"message_type"`
			SubscriptionType string `json:"subscription_type"`
		} `json:"metadata"`
		Payload struct {
			Event struct {
				BroadcasterID    string `json:"broadcaster_user_id"`
				ChatterName      string `json:"chatter_user_name"`
				ChatterLogin     string `json:"chatter_user_login"`
				ChatterAnonymous bool   `json:"chatter_is_anonymous"`
				NoticeType       string `json:"notice_type"`
				CommunitySubGift *struct {
					ID    string `json:"id"`
					Total int    `json:"total"`
				} `json:"community_sub_gift"`
				SubGift *struct {
					CommunityID    string `json:"community_gift_id"`
					RecipientID    string `json:"recipient_user_id"`
					RecipientName  string `json:"recipient_user_name"`
					RecipientLogin string `json:"recipient_user_login"`
				} `json:"sub_gift"`
			} `json:"event"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return Event{}, false, err
	}
	if m.Metadata.MessageType != "notification" || m.Metadata.SubscriptionType != "channel.chat.notification" || m.Payload.Event.BroadcasterID != broadcasterID {
		return Event{}, false, nil
	}
	e := m.Payload.Event
	switch e.NoticeType {
	case "community_sub_gift":
		if e.CommunitySubGift == nil || e.CommunitySubGift.ID == "" || e.CommunitySubGift.Total < 10 || e.CommunitySubGift.Total > 1000 {
			return Event{}, false, nil
		}
		gifter := cleanName(e.ChatterName)
		if gifter == "" {
			gifter = cleanName(e.ChatterLogin)
		}
		if e.ChatterAnonymous || gifter == "" {
			gifter = "ANONYMOUS"
		}
		return Event{Kind: KindStart, CommunityID: e.CommunitySubGift.ID, Total: e.CommunitySubGift.Total, Gifter: gifter, Anonymous: e.ChatterAnonymous}, true, nil
	case "sub_gift":
		if e.SubGift == nil || e.SubGift.CommunityID == "" || e.SubGift.RecipientID == "" {
			return Event{}, false, nil
		}
		name := cleanName(e.SubGift.RecipientName)
		if name == "" {
			name = cleanName(e.SubGift.RecipientLogin)
		}
		if name == "" {
			return Event{}, false, nil
		}
		return Event{Kind: KindRecipient, CommunityID: e.SubGift.CommunityID, Recipient: name, RecipientID: e.SubGift.RecipientID}, true, nil
	default:
		return Event{}, false, nil
	}
}

func ParseTestCommand(messageID, chatterID, broadcasterID, text string) (Celebration, bool, error) {
	fields := strings.Fields(text)
	if chatterID != broadcasterID || messageID == "" || len(fields) < 2 || !strings.EqualFold(fields[0], "!testgift") {
		return Celebration{}, false, nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), fields[0]))
	parts := strings.Split(rest, "|")
	if len(parts) != 2 {
		return Celebration{}, false, nil
	}
	gifter := cleanName(parts[0])
	if gifter == "" {
		return Celebration{}, false, errors.New("test gift requires a gifter")
	}
	seen := map[string]bool{}
	var names []string
	for _, raw := range strings.Split(parts[1], ",") {
		name := cleanName(raw)
		key := strings.ToLower(name)
		if name != "" && !seen[key] {
			seen[key] = true
			names = append(names, name)
		}
	}
	if len(names) < 10 || len(names) > 100 {
		return Celebration{}, false, nil
	}
	return Celebration{CommunityID: "testgift:" + messageID, Total: len(names), Gifter: gifter, Recipients: names, Test: true}, true, nil
}

type pending struct {
	Celebration
	started time.Time
	seen    map[string]bool
}

type Collector struct {
	window    time.Duration
	pending   map[string]*pending
	completed map[string]bool
	order     []string
}

func NewCollector(window time.Duration) *Collector {
	return &Collector{window: window, pending: map[string]*pending{}, completed: map[string]bool{}}
}

func (c *Collector) Accept(e Event, now time.Time) []Celebration {
	if e.CommunityID == "" || c.completed[e.CommunityID] {
		return nil
	}
	p := c.pending[e.CommunityID]
	if p == nil {
		p = &pending{Celebration: Celebration{CommunityID: e.CommunityID}, started: now, seen: map[string]bool{}}
		c.pending[e.CommunityID] = p
		c.order = append(c.order, e.CommunityID)
	}
	if e.Kind == KindStart && p.Total == 0 {
		p.Total, p.Gifter = e.Total, e.Gifter
		p.started = now
	}
	if e.Kind == KindRecipient {
		key := e.RecipientID
		if key == "" {
			key = strings.ToLower(e.Recipient)
		}
		if e.Recipient != "" && !p.seen[key] {
			p.seen[key] = true
			p.Recipients = append(p.Recipients, e.Recipient)
		}
	}
	if p.Total > 0 && len(p.Recipients) >= p.Total {
		return []Celebration{c.finish(e.CommunityID)}
	}
	return nil
}

func (c *Collector) FlushDue(now time.Time) []Celebration {
	var out []Celebration
	for _, id := range c.order {
		p := c.pending[id]
		if p == nil {
			continue
		}
		if p.Total > 0 && !now.Before(p.started.Add(c.window)) {
			out = append(out, c.finish(id))
		}
		if p.Total == 0 && !now.Before(p.started.Add(2*c.window)) {
			delete(c.pending, id)
		}
	}
	return out
}

func (c *Collector) finish(id string) Celebration {
	p := c.pending[id]
	delete(c.pending, id)
	c.completed[id] = true
	p.Missing = p.Total - len(p.Recipients)
	if p.Missing < 0 {
		p.Missing = 0
	}
	return p.Celebration
}

func (c *Collector) NextDue() (time.Time, bool) {
	var best time.Time
	for _, p := range c.pending {
		if p.Total == 0 {
			continue
		}
		due := p.started.Add(c.window)
		if best.IsZero() || due.Before(best) {
			best = due
		}
	}
	return best, !best.IsZero()
}

func (c *Collector) HasPending() bool { return len(c.pending) > 0 }

func CleanName(raw string) string { return cleanName(raw) }
