package twitchgift

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/twitchbanner"
)

type CommandRunner func(context.Context, string, ...string) ([]byte, error)

type GiftPrinter struct {
	Binary string
	Queue  string
	Run    CommandRunner
}

type PrintResult struct {
	JobID string
}

func (p GiftPrinter) Print(ctx context.Context, c Celebration) (PrintResult, error) {
	run := p.Run
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		}
	}
	args := []string{"--queue", p.Queue, "--gifter", c.Gifter, "--total", strconv.Itoa(c.Total), "--missing", strconv.Itoa(c.Missing), "--recipients", strings.Join(c.Recipients, ",")}
	out, err := run(ctx, p.Binary, args...)
	if err != nil {
		return PrintResult{}, fmt.Errorf("giftprint: %w: %s", err, out)
	}
	var r struct {
		OK        bool   `json:"ok"`
		Submitted bool   `json:"submitted"`
		JobID     string `json:"job_id"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return PrintResult{}, err
	}
	if !r.OK || !r.Submitted {
		return PrintResult{}, errors.New("giftprint did not confirm submission")
	}
	return PrintResult{JobID: r.JobID}, nil
}

type ProcessResult struct {
	Duplicate bool
	Submitted bool
	JobID     string
}

type Processor struct {
	Journal *twitchbanner.Journal
	Printer GiftPrinter
}

func (p Processor) Process(ctx context.Context, c Celebration) (ProcessResult, error) {
	id := c.CommunityID
	if !strings.HasPrefix(id, "testgift:") {
		id = "gift:" + id
	}
	summary := fmt.Sprintf("%d gift subs; %d named; %d missing", c.Total, len(c.Recipients), c.Missing)
	ok, err := p.Journal.ReserveEvent(id, c.Total, c.Gifter, summary)
	if err != nil {
		return ProcessResult{}, err
	}
	if !ok {
		return ProcessResult{Duplicate: true}, nil
	}
	r, err := p.Printer.Print(ctx, c)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("print status unknown; gift is reserved and will not be retried: %w", err)
	}
	if err := p.Journal.Submitted(id, r.JobID, summary); err != nil {
		return ProcessResult{Submitted: true, JobID: r.JobID}, err
	}
	return ProcessResult{Submitted: true, JobID: r.JobID}, nil
}

func ParseTestNotification(data []byte, broadcasterID string) (Celebration, bool, error) {
	var m struct {
		Metadata struct {
			MessageType      string `json:"message_type"`
			SubscriptionType string `json:"subscription_type"`
		} `json:"metadata"`
		Payload struct {
			Event struct {
				BroadcasterID string `json:"broadcaster_user_id"`
				ChatterID     string `json:"chatter_user_id"`
				MessageID     string `json:"message_id"`
				Message       struct {
					Text string `json:"text"`
				} `json:"message"`
			} `json:"event"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return Celebration{}, false, err
	}
	if m.Metadata.MessageType != "notification" || m.Metadata.SubscriptionType != "channel.chat.message" || m.Payload.Event.BroadcasterID != broadcasterID {
		return Celebration{}, false, nil
	}
	e := m.Payload.Event
	return ParseTestCommand(e.MessageID, e.ChatterID, broadcasterID, e.Message.Text)
}
