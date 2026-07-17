package twitchbanner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const MaxTextBytes = 256
const MaxWords = 16

type Cheer struct {
	Bits      int    `json:"bits"`
	UserName  string `json:"user_name"`
	Message   string `json:"message"`
	Anonymous bool   `json:"is_anonymous"`
}

type Envelope struct {
	MessageID string
	Cheer     Cheer
}

type Decision struct {
	Qualifies bool
	Text      string
}

type PrintResult struct {
	JobID string `json:"job_id"`
}
type ProcessResult struct {
	Duplicate bool
	Submitted bool
	JobID     string
	Text      string
}

type Printer interface {
	Print(context.Context, string) (PrintResult, error)
}

type Journal struct {
	mu   sync.Mutex
	path string
	seen map[string]bool
}

type journalEntry struct {
	EventID string `json:"event_id"`
	State   string `json:"state"`
	Bits    int    `json:"bits,omitempty"`
	User    string `json:"user,omitempty"`
	Text    string `json:"text,omitempty"`
	JobID   string `json:"job_id,omitempty"`
}

func OpenJournal(path string) (*Journal, error) {
	j := &Journal{path: path, seen: map[string]bool{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	line := 0
	for s.Scan() {
		line++
		if len(bytes.TrimSpace(s.Bytes())) == 0 {
			return nil, fmt.Errorf("journal line %d is empty; refusing to start", line)
		}
		var e journalEntry
		if err := json.Unmarshal(s.Bytes(), &e); err != nil || e.EventID == "" || (e.State != "reserved" && e.State != "submitted") {
			return nil, fmt.Errorf("journal line %d is malformed; refusing to start", line)
		}
		j.seen[e.EventID] = true
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *Journal) append(e journalEntry) error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(j.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func (j *Journal) Reserve(id string, cheer Cheer) (bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if id == "" {
		return false, errors.New("missing Twitch message ID")
	}
	if j.seen[id] {
		return false, nil
	}
	e := journalEntry{EventID: id, State: "reserved", Bits: cheer.Bits, User: cheer.UserName, Text: cheer.Message}
	if err := j.append(e); err != nil {
		return false, err
	}
	j.seen[id] = true
	return true, nil
}

func (j *Journal) Submitted(id, jobID, text string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.append(journalEntry{EventID: id, State: "submitted", JobID: jobID, Text: text})
}

func cleanWord(word string) string {
	var b strings.Builder
	for _, r := range word {
		if unicode.IsPrint(r) && !unicode.Is(unicode.M, r) && r != '\u200b' && r != '\u200d' {
			if r >= 32 && r <= 126 || unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), "")
}

func boundedText(raw string) string {
	words := strings.Fields(raw)
	out := make([]string, 0, MaxWords)
	bytes := 0
	for _, word := range words {
		word = cleanWord(word)
		if word == "" {
			continue
		}
		need := len([]byte(word))
		if len(out) > 0 {
			need++
		}
		if len(out) == MaxWords || bytes+need > MaxTextBytes {
			break
		}
		out = append(out, word)
		bytes += need
	}
	return strings.Join(out, " ")
}

func PrepareCheer(c Cheer) Decision {
	if c.Bits < 1000 {
		return Decision{}
	}
	text := boundedText(c.Message)
	if text == "" {
		name := c.UserName
		if c.Anonymous || strings.TrimSpace(name) == "" {
			name = "ANONYMOUS"
		}
		name = strings.ToUpper(boundedText(name))
		if name == "" {
			name = "ANONYMOUS"
		}
		text = "THANK YOU " + name
	}
	return Decision{Qualifies: true, Text: text}
}

type Processor struct {
	Journal *Journal
	Printer Printer
}

func (p Processor) Process(ctx context.Context, env Envelope) (ProcessResult, error) {
	d := PrepareCheer(env.Cheer)
	if !d.Qualifies {
		return ProcessResult{}, nil
	}
	ok, err := p.Journal.Reserve(env.MessageID, env.Cheer)
	if err != nil {
		return ProcessResult{}, err
	}
	if !ok {
		return ProcessResult{Duplicate: true}, nil
	}
	r, err := p.Printer.Print(ctx, d.Text)
	if err != nil {
		return ProcessResult{Text: d.Text}, fmt.Errorf("print status unknown; event is reserved and will not be retried: %w", err)
	}
	if err := p.Journal.Submitted(env.MessageID, r.JobID, d.Text); err != nil {
		return ProcessResult{Submitted: true, JobID: r.JobID, Text: d.Text}, err
	}
	return ProcessResult{Submitted: true, JobID: r.JobID, Text: d.Text}, nil
}

func ParseNotification(data []byte) (Envelope, bool, error) {
	var header struct {
		Metadata struct {
			MessageID        string `json:"message_id"`
			MessageType      string `json:"message_type"`
			SubscriptionType string `json:"subscription_type"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Envelope{}, false, err
	}
	if header.Metadata.MessageType != "notification" || header.Metadata.SubscriptionType != "channel.cheer" {
		return Envelope{}, false, nil
	}
	var body struct {
		Payload struct {
			Event Cheer `json:"event"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return Envelope{}, false, err
	}
	return Envelope{MessageID: header.Metadata.MessageID, Cheer: body.Payload.Event}, true, nil
}

func ParseChatCommand(data []byte, broadcasterID string) (Envelope, bool, error) {
	var m struct {
		Metadata struct {
			MessageType      string `json:"message_type"`
			SubscriptionType string `json:"subscription_type"`
		} `json:"metadata"`
		Payload struct {
			Event struct {
				BroadcasterID string `json:"broadcaster_user_id"`
				ChatterID     string `json:"chatter_user_id"`
				ChatterLogin  string `json:"chatter_user_login"`
				MessageID     string `json:"message_id"`
				Message       struct {
					Text string `json:"text"`
				} `json:"message"`
			} `json:"event"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return Envelope{}, false, err
	}
	if m.Metadata.MessageType != "notification" || m.Metadata.SubscriptionType != "channel.chat.message" {
		return Envelope{}, false, nil
	}
	e := m.Payload.Event
	if e.BroadcasterID != broadcasterID || e.ChatterID != broadcasterID || e.MessageID == "" {
		return Envelope{}, false, nil
	}
	fields := strings.Fields(e.Message.Text)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "!testbanner") {
		return Envelope{}, false, nil
	}
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(e.Message.Text), fields[0]))
	if text == "" {
		return Envelope{}, false, nil
	}
	return Envelope{MessageID: "chat:" + e.MessageID, Cheer: Cheer{Bits: 1000, UserName: e.ChatterLogin, Message: text}}, true, nil
}

type OAuthCallback struct {
	Code  string
	Error string
}

func OAuthCallbackHandler(state string, results chan<- OAuthCallback) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
			return
		}
		res := OAuthCallback{Code: r.URL.Query().Get("code"), Error: r.URL.Query().Get("error")}
		if res.Code == "" {
			http.Error(w, "Authorization failed", http.StatusBadRequest)
			return
		}
		select {
		case results <- res:
		default:
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "Twitch authorization complete. You may close this window.")
	})
}

type CommandRunner func(context.Context, string, ...string) ([]byte, error)
type BannerPrinter struct {
	Binary, Queue string
	Run           CommandRunner
}

func (p BannerPrinter) Print(ctx context.Context, text string) (PrintResult, error) {
	run := p.Run
	if run == nil {
		run = func(ctx context.Context, n string, a ...string) ([]byte, error) {
			return exec.CommandContext(ctx, n, a...).CombinedOutput()
		}
	}
	out, err := run(ctx, p.Binary, "--queue", p.Queue, "--lines", "auto", "--", text)
	if err != nil {
		return PrintResult{}, fmt.Errorf("bannerprint: %w: %s", err, out)
	}
	var r struct {
		OK, Submitted bool
		JobID         string `json:"job_id"`
	}
	if err = json.Unmarshal(out, &r); err != nil {
		return PrintResult{}, err
	}
	if !r.OK || !r.Submitted {
		return PrintResult{}, errors.New("bannerprint did not confirm submission")
	}
	return PrintResult{JobID: r.JobID}, nil
}

func RuneSafePrefix(s string, n int) string {
	for len(s) > n {
		s = s[:len(s)-1]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}
