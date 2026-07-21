package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/protocol"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/raidpage"
)

type config struct {
	Queue, Channel, PreviewPNG string
	Viewers                    int
	Preview                    bool
}
type report struct {
	OK, Preview, Submitted          bool
	Queue, Channel, JobID, SHA256   string
	Viewers, WidthDots, Rows, Bytes int
}
type runner func(string, ...string) ([]byte, error)
type submitter func(string, []byte, ...string) ([]byte, error)

var queuePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,126}$`)
var jobPattern = regexp.MustCompile(`request id is ([A-Za-z0-9_.-]+-[0-9]+)\b`)

func parseArgs(args []string, env string) (config, error) {
	c := config{Queue: env}
	fs := flag.NewFlagSet("raidprint", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.Queue, "queue", c.Queue, "CUPS queue")
	fs.StringVar(&c.Channel, "channel", "", "raiding channel")
	fs.IntVar(&c.Viewers, "viewers", 0, "raider count")
	fs.BoolVar(&c.Preview, "preview", false, "preview")
	fs.StringVar(&c.PreviewPNG, "preview-png", "", "PNG preview")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	if fs.NArg() != 0 || c.Channel == "" || c.Viewers <= 0 || c.Viewers > 1000000 {
		return c, errors.New("channel, viewers, and no positional arguments required")
	}
	if c.Queue != "" && !queuePattern.MatchString(c.Queue) {
		return c, errors.New("invalid queue")
	}
	if !c.Preview && c.PreviewPNG != "" {
		return c, errors.New("preview-png requires preview")
	}
	return c, nil
}
func resolveQueue(q string, r runner) (string, error) {
	if q != "" {
		if !strings.HasPrefix(q, "Rongta_F11") || !queuePattern.MatchString(q) {
			return "", errors.New("queue must be F11")
		}
		return q, nil
	}
	o, e := r("lpstat", "-d")
	if e != nil {
		return "", e
	}
	p := "system default destination: "
	line := strings.TrimSpace(string(o))
	if !strings.HasPrefix(line, p) {
		return "", errors.New("no default queue")
	}
	return resolveQueue(strings.TrimPrefix(line, p), r)
}
func verifyQueue(q string, r runner) error {
	o, e := r("lpstat", "-v", q)
	if e != nil {
		return e
	}
	line := strings.TrimSpace(string(o))
	if !strings.HasPrefix(line, "device for "+q+": ") {
		return errors.New("unexpected queue identity")
	}
	o, e = r("/usr/local/lib/f11/check-f11-runtime", q, line, "/sys/bus/usb/devices")
	if e != nil || strings.TrimSpace(string(o)) == "" {
		return errors.New("queue is not attached F11")
	}
	return nil
}
func build(c config) ([]byte, []byte, error) {
	gray, _, e := raidpage.Render(raidpage.Receipt{Channel: c.Channel, Viewers: c.Viewers})
	if e != nil {
		return nil, nil, e
	}
	intended, e := protocol.NativeMonochrome(gray, raidpage.Width, raidpage.Height)
	if e != nil {
		return nil, nil, e
	}
	stream, e := protocol.EncodeNativeJob(gray, raidpage.Width, raidpage.Height, protocol.Settings{Speed: 12, Density: 9, Copies: 1})
	if e != nil {
		return nil, nil, e
	}
	d, e := protocol.DecodeJob(stream)
	if e != nil || d.WidthBytes != raidpage.Width/8 || d.Height != raidpage.Height || d.Copies != 1 || len(d.Rows) != len(intended) {
		return nil, nil, errors.New("decoded raid geometry mismatch")
	}
	for i := range intended {
		if !bytes.Equal(intended[i], d.Rows[i]) {
			return nil, nil, fmt.Errorf("raid raster mismatch row %d", i+1)
		}
	}
	return stream, gray, nil
}
func run(args []string, env string, out io.Writer, r runner, s submitter) error {
	c, e := parseArgs(args, env)
	if e != nil {
		return e
	}
	stream, gray, e := build(c)
	if e != nil {
		return e
	}
	if c.PreviewPNG != "" {
		img := image.NewGray(image.Rect(0, 0, raidpage.Width, raidpage.Height))
		copy(img.Pix, gray)
		f, e := os.OpenFile(c.PreviewPNG, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		if e = png.Encode(f, img); e != nil {
			f.Close()
			return e
		}
		if e = f.Close(); e != nil {
			return e
		}
	}
	sum := sha256.Sum256(stream)
	result := report{OK: true, Preview: c.Preview, Queue: c.Queue, Channel: c.Channel, Viewers: c.Viewers, WidthDots: raidpage.Width, Rows: raidpage.Height, Bytes: len(stream), SHA256: hex.EncodeToString(sum[:])}
	if c.Preview && result.Queue == "" {
		result.Queue = "auto"
	}
	if !c.Preview {
		c.Queue, e = resolveQueue(c.Queue, r)
		if e != nil {
			return e
		}
		if e = verifyQueue(c.Queue, r); e != nil {
			return e
		}
		result.Queue = c.Queue
		o, e := s("lp", stream, "-n", "1", "-d", c.Queue, "-o", "raw", "-t", "Twitch raid: "+c.Channel, "-")
		if e != nil {
			return fmt.Errorf("CUPS status unknown; do not retry: %w", e)
		}
		result.Submitted = true
		if m := jobPattern.FindSubmatch(o); m != nil {
			result.JobID = string(m[1])
		}
	}
	return json.NewEncoder(out).Encode(result)
}
func cmd(n string, a ...string) *exec.Cmd {
	c := exec.Command(n, a...)
	c.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	return c
}
func main() {
	if e := run(os.Args[1:], os.Getenv("F11_QUEUE"), os.Stdout, func(n string, a ...string) ([]byte, error) { return cmd(n, a...).CombinedOutput() }, func(n string, b []byte, a ...string) ([]byte, error) {
		c := cmd(n, a...)
		c.Stdin = bytes.NewReader(b)
		return c.CombinedOutput()
	}); e != nil {
		fmt.Fprintln(os.Stderr, "raidprint:", e)
		os.Exit(2)
	}
}
