package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/protocol"
)

func validRunner(t *testing.T) runner {
	return func(name string, args ...string) ([]byte, error) {
		switch name {
		case "lpstat":
			if !reflect.DeepEqual(args, []string{"-v", "Rongta_F11_Media"}) {
				t.Fatalf("args=%#v", args)
			}
			return []byte("device for Rongta_F11_Media: usb:///F11?serial=TEST\n"), nil
		case "/usr/local/lib/f11/check-f11-runtime":
			return []byte("usb:///F11?serial=TEST\n"), nil
		default:
			t.Fatalf("unexpected command %q", name)
			return nil, nil
		}
	}
}

func TestRaidPrintPreviewDoesNotSubmit(t *testing.T) {
	var out bytes.Buffer
	called := false
	err := run([]string{"--preview", "--channel", "Raid Channel", "--viewers", "47"}, "", &out, func(string, ...string) ([]byte, error) { called = true; return nil, nil }, func(string, []byte, ...string) ([]byte, error) { called = true; return nil, nil })
	if err != nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	var got report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || !got.Preview || got.Submitted || got.Channel != "Raid Channel" || got.Viewers != 47 || got.Rows != 2233 {
		t.Fatalf("report=%#v", got)
	}
}

func TestRaidPrintSubmitsOneValidatedRawLetterPage(t *testing.T) {
	var out bytes.Buffer
	calls := 0
	err := run([]string{"--queue", "Rongta_F11_Media", "--channel", "Raid Channel", "--viewers", "47"}, "", &out, validRunner(t), func(name string, stream []byte, args ...string) ([]byte, error) {
		calls++
		if name != "lp" || !reflect.DeepEqual(args, []string{"-n", "1", "-d", "Rongta_F11_Media", "-o", "raw", "-t", "Twitch raid: Raid Channel", "-"}) {
			t.Fatalf("name=%q args=%#v", name, args)
		}
		job, err := protocol.DecodeJob(stream)
		if err != nil {
			t.Fatal(err)
		}
		if job.WidthBytes != 208 || job.Height != 2233 || job.Copies != 1 {
			t.Fatalf("job=%#v", job)
		}
		return []byte("request id is Rongta_F11_Media-99 (1 file(s))\n"), nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRaidPrintRejectsInvalidInputsAndDoesNotRetry(t *testing.T) {
	for _, args := range [][]string{{"--channel", "", "--viewers", "5"}, {"--channel", "Raid", "--viewers", "0"}, {"--channel", "Raid💚", "--viewers", "5"}} {
		if err := run(args, "", &bytes.Buffer{}, nil, nil); err == nil {
			t.Fatalf("accepted %#v", args)
		}
	}
	err := run([]string{"--queue", "Rongta_F11_Media", "--channel", "Raid", "--viewers", "5"}, "", &bytes.Buffer{}, validRunner(t), func(string, []byte, ...string) ([]byte, error) { return nil, errors.New("unknown") })
	if err == nil || !strings.Contains(err.Error(), "do not retry") {
		t.Fatalf("err=%v", err)
	}
}

func TestRaidPrintCreatesDeterministicPreview(t *testing.T) {
	for _, name := range []string{"/tmp/raid-preview-a.png", "/tmp/raid-preview-b.png"} {
		_ = name
	}
	// build itself performs the native stream round-trip; this test asserts the
	// source inputs produce a stable validated page before filesystem output.
	a, _, err := build(config{Channel: "Raid Channel", Viewers: 47})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := build(config{Channel: "Raid Channel", Viewers: 47})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("raid stream is nondeterministic")
	}
}
