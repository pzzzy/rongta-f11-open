package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/protocol"
)

func TestParseDefaultsAndForcedOptions(t *testing.T) {
	cfg, err := parseArgs([]string{"--lines", "2", "--font", "comic-sans", "PLEASE", "WAIT"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Queue != "" || cfg.LineCount != 2 || cfg.Font != "comic-sans" || cfg.Text != "PLEASE WAIT" || cfg.Preview {
		t.Fatalf("cfg=%#v", cfg)
	}
	cfg, err = parseArgs([]string{"--preview", "HELLO"}, "Custom_Queue")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Queue != "Custom_Queue" || !cfg.Preview || cfg.LineCount != 0 || cfg.Font != "bold" {
		t.Fatalf("cfg=%#v", cfg)
	}
}

func TestResolveQueueUsesOnlyAnF11SystemDefault(t *testing.T) {
	runner := func(name string, args ...string) ([]byte, error) {
		if name != "lpstat" || !reflect.DeepEqual(args, []string{"-d"}) {
			t.Fatalf("command=%q args=%#v", name, args)
		}
		return []byte("system default destination: Rongta_F11_Media\n"), nil
	}
	queue, err := resolveQueue("", runner)
	if err != nil || queue != "Rongta_F11_Media" {
		t.Fatalf("queue=%q err=%v", queue, err)
	}
	if _, err := resolveQueue("", func(string, ...string) ([]byte, error) {
		return []byte("system default destination: Office_Laser\n"), nil
	}); err == nil {
		t.Fatal("accepted non-F11 default")
	}
}

func TestParseRejectsUnsafeOrUnboundedInputs(t *testing.T) {
	cases := [][]string{{}, {"--lines", "4", "HELLO"}, {"--font", "papyrus", "HELLO"}, {"--queue", "-evil", "HELLO"}, {"--queue", "bad/queue", "HELLO"}, {strings.Repeat("X", 257)}, {strings.Repeat("X ", 17)}, {"--copies", "2", "HELLO"}}
	for _, args := range cases {
		if _, err := parseArgs(args, ""); err == nil {
			t.Fatalf("accepted %#v", args)
		}
	}
}

func TestPreviewBuildsAndValidatesWithoutCallingExternalCommands(t *testing.T) {
	var out bytes.Buffer
	called := false
	runner := func(string, ...string) ([]byte, error) { called = true; return nil, nil }
	submitter := func(string, []byte, ...string) ([]byte, error) { called = true; return nil, nil }
	if err := run([]string{"--preview", "--lines", "3", "MEETING", "IN", "PROGRESS", "PLEASE", "WAIT"}, "", &out, runner, submitter); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("preview invoked external command")
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || !got.Preview || got.Submitted || got.Rows != 3045 || got.WidthDots != 1664 || len(got.Lines) != 3 || got.Font != "bold" || got.Bytes <= 0 || got.SHA256 == "" {
		t.Fatalf("report=%#v", got)
	}
}

func validQueueRunner(t *testing.T) commandRunner {
	return func(name string, args ...string) ([]byte, error) {
		switch name {
		case "lpstat":
			if !reflect.DeepEqual(args, []string{"-v", "Rongta_F11_Media"}) {
				t.Fatalf("lpstat args=%#v", args)
			}
			return []byte("device for Rongta_F11_Media: usb:///F11?serial=TEST123\n"), nil
		case "/usr/local/lib/f11/check-f11-runtime":
			if len(args) != 3 || args[0] != "Rongta_F11_Media" {
				t.Fatalf("helper args=%#v", args)
			}
			return []byte("usb:///F11?serial=TEST123\n"), nil
		default:
			t.Fatalf("unexpected command %q %#v", name, args)
			return nil, nil
		}
	}
}

func TestVerifyQueueRejectsMissingSpoofedAndWrongDevice(t *testing.T) {
	cases := []commandRunner{
		func(string, ...string) ([]byte, error) { return nil, errors.New("missing") },
		func(name string, args ...string) ([]byte, error) {
			if name == "lpstat" {
				return []byte("device for Rongta_F11_Evil: socket://attacker\n"), nil
			}
			return nil, errors.New("not an F11 USB URI")
		},
		func(name string, args ...string) ([]byte, error) {
			if name == "lpstat" {
				return []byte("device for Rongta_F11_Evil: usb:///F11?serial=WRONG\n"), nil
			}
			return nil, errors.New("no matching USB device")
		},
	}
	for i, runner := range cases {
		if err := verifyQueue("Rongta_F11_Evil", runner); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestPrintSubmitsOneValidatedRawJobViaStdin(t *testing.T) {
	var out bytes.Buffer
	calls := 0
	var gotArgs []string
	submitter := func(name string, stream []byte, args ...string) ([]byte, error) {
		calls++
		if name != "lp" {
			t.Fatalf("name=%q", name)
		}
		gotArgs = append([]string(nil), args...)
		decoded, err := protocol.DecodeJob(stream)
		if err != nil {
			t.Fatal(err)
		}
		if decoded.WidthBytes != 208 || decoded.Height != 3045 || decoded.Copies != 1 {
			t.Fatalf("decoded=%#v", decoded)
		}
		return []byte("request id is Rongta_F11_Media-42 (1 file(s))\n"), nil
	}
	if err := run([]string{"--queue", "Rongta_F11_Media", "--font", "comic-sans", "NO", "PARKING"}, "", &out, validQueueRunner(t), submitter); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	want := []string{"-n", "1", "-d", "Rongta_F11_Media", "-o", "raw", "-t", "bannerprint: NO PARKING", "-"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("args=%#v", gotArgs)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || !got.Submitted || got.Preview || got.Font != "comic-sans" || got.JobID != "Rongta_F11_Media-42" {
		t.Fatalf("report=%#v", got)
	}
}

func TestSuccessfulSubmissionWithUnexpectedOutputIsNotFailure(t *testing.T) {
	var out bytes.Buffer
	submitter := func(string, []byte, ...string) ([]byte, error) { return []byte("localized successful output\n"), nil }
	if err := run([]string{"--queue", "Rongta_F11_Media", "HELLO"}, "", &out, validQueueRunner(t), submitter); err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Submitted || got.JobID != "" {
		t.Fatalf("report=%#v", got)
	}
}

func TestSubmissionErrorWarnsAgainstRetry(t *testing.T) {
	var out bytes.Buffer
	submitter := func(string, []byte, ...string) ([]byte, error) { return nil, errors.New("transport error") }
	err := run([]string{"--queue", "Rongta_F11_Media", "HELLO"}, "", &out, validQueueRunner(t), submitter)
	if err == nil || !strings.Contains(err.Error(), "do not retry automatically") {
		t.Fatalf("err=%v", err)
	}
}

func TestMaximumAutoInputCompletesWithinLocalBudget(t *testing.T) {
	text := strings.TrimSpace(strings.Repeat("X ", 16))
	cfg, err := parseArgs([]string{"--preview", text}, "")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, _, err := build(cfg); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("elapsed=%s", elapsed)
	}
}

func TestRejectsInvalidUnsupportedAndUnshapedText(t *testing.T) {
	cases := []string{
		string([]byte{0xff, 'X'}),
		"LINE\nBREAK",
		"TAB\tTEXT",
		"HELLO 😀",
		"漢字",
		"e\u0301",
		"का",
		"ZERO\u200bWIDTH",
	}
	for _, text := range cases {
		if _, err := parseArgs([]string{"--preview", text}, ""); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	if _, err := parseArgs([]string{"--preview", "CAFÉ"}, ""); err != nil {
		t.Fatalf("rejected supported printable text: %v", err)
	}
}

func TestJobTitleTruncatesOnRuneBoundary(t *testing.T) {
	title := jobTitle(strings.Repeat("É", 60))
	if !utf8.ValidString(title) || len(title) > 80 {
		t.Fatalf("invalid title bytes=%d %q", len(title), title)
	}
}

func TestTextBeginningWithDashRequiresOptionTerminatorButRemainsData(t *testing.T) {
	cfg, err := parseArgs([]string{"--preview", "--", "-DO", "NOT", "ENTER"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Text != "-DO NOT ENTER" {
		t.Fatalf("text=%q", cfg.Text)
	}
}
