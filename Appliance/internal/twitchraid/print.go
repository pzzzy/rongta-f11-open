package twitchraid

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/protocol"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/raidpage"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/twitchbanner"
)

type CommandRunner func(context.Context, string, []byte, ...string) ([]byte, error)

type Printer struct {
	Binary string
	Queue  string
	Run    CommandRunner
}

type ProcessResult struct {
	Duplicate bool
	Submitted bool
	JobID     string
}

var jobPattern = regexp.MustCompile(`request id is ([A-Za-z0-9_.-]+-[0-9]+)\b`)

func (p Printer) Print(ctx context.Context, e Event) (string, error) {
	gray, _, err := raidpage.Render(raidpage.Receipt{Channel: e.Channel, Viewers: e.Viewers})
	if err != nil {
		return "", err
	}
	intended, err := protocol.NativeMonochrome(gray, raidpage.Width, raidpage.Height)
	if err != nil {
		return "", err
	}
	stream, err := protocol.EncodeNativeJob(gray, raidpage.Width, raidpage.Height, protocol.Settings{Speed: 12, Density: 9, Copies: 1})
	if err != nil {
		return "", err
	}
	decoded, err := protocol.DecodeJob(stream)
	if err != nil || decoded.WidthBytes != raidpage.Width/8 || decoded.Height != raidpage.Height || decoded.Copies != 1 {
		return "", errors.New("raid stream geometry mismatch")
	}
	for i := range intended {
		if !bytes.Equal(intended[i], decoded.Rows[i]) {
			return "", fmt.Errorf("raid raster mismatch row %d", i+1)
		}
	}
	run := p.Run
	if run == nil {
		run = func(ctx context.Context, name string, input []byte, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdin = bytes.NewReader(input)
			return cmd.CombinedOutput()
		}
	}
	out, err := run(ctx, p.Binary, stream, "-n", "1", "-d", p.Queue, "-o", "raw", "-t", "Twitch raid: "+e.Channel, "-")
	if err != nil {
		return "", fmt.Errorf("CUPS status unknown; do not retry: %w", err)
	}
	match := jobPattern.FindSubmatch(out)
	if match == nil {
		return "", errors.New("raidprint output did not include a CUPS job ID")
	}
	return string(match[1]), nil
}

type Processor struct {
	Journal *twitchbanner.Journal
	Printer Printer
}

func (p Processor) Process(ctx context.Context, e Event) (ProcessResult, error) {
	ok, err := p.Journal.ReserveEvent(e.MessageID, e.Viewers, e.Channel, fmt.Sprintf("raid viewers=%d", e.Viewers))
	if err != nil {
		return ProcessResult{}, err
	}
	if !ok {
		return ProcessResult{Duplicate: true}, nil
	}
	job, err := p.Printer.Print(ctx, e)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("raid is reserved and will not be retried: %w", err)
	}
	if err := p.Journal.Submitted(e.MessageID, job, fmt.Sprintf("%s raid viewers=%d", e.Channel, e.Viewers)); err != nil {
		return ProcessResult{Submitted: true, JobID: job}, err
	}
	return ProcessResult{Submitted: true, JobID: job}, nil
}
