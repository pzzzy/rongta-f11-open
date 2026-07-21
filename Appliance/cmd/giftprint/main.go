package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/giftpage"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/protocol"
)

type config struct {
	Queue, Gifter, PreviewPNG, PreviewPGM string
	Total, Missing                        int
	Recipients                            []string
	Preview                               bool
}
type report struct {
	OK        bool   `json:"ok"`
	Preview   bool   `json:"preview"`
	Submitted bool   `json:"submitted"`
	Queue     string `json:"queue"`
	Gifter    string `json:"gifter"`
	JobID     string `json:"job_id,omitempty"`
	SHA256    string `json:"sha256"`
	Total     int    `json:"total"`
	Displayed int    `json:"displayed"`
	Missing   int    `json:"missing"`
	More      int    `json:"more"`
	WidthDots int    `json:"width_dots"`
	Rows      int    `json:"rows"`
	Bytes     int    `json:"bytes"`
}
type runner func(string, ...string) ([]byte, error)
type submitter func(string, []byte, ...string) ([]byte, error)

var queuePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,126}$`)
var jobPattern = regexp.MustCompile(`request id is ([A-Za-z0-9_.-]+-[0-9]+)\b`)

func parseArgs(args []string, envQueue string) (config, error) {
	c := config{Queue: envQueue}
	fs := flag.NewFlagSet("giftprint", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.Queue, "queue", c.Queue, "CUPS queue")
	fs.StringVar(&c.Gifter, "gifter", "", "gifter display name")
	fs.IntVar(&c.Total, "total", 0, "gift count")
	fs.IntVar(&c.Missing, "missing", 0, "recipient names not received")
	fs.BoolVar(&c.Preview, "preview", false, "render without printing")
	fs.StringVar(&c.PreviewPNG, "preview-png", "", "write PNG preview")
	fs.StringVar(&c.PreviewPGM, "preview-pgm", "", "write PGM preview")
	var recipients string
	fs.StringVar(&recipients, "recipients", "", "comma-separated giftee names")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	if fs.NArg() != 0 {
		return c, errors.New("unexpected positional arguments")
	}
	if c.Queue != "" && !queuePattern.MatchString(c.Queue) {
		return c, errors.New("invalid queue")
	}
	if c.Total < 10 || c.Total > 1000 || c.Missing < 0 {
		return c, errors.New("total must be 10..1000 and missing nonnegative")
	}
	for _, n := range strings.Split(recipients, ",") {
		if strings.TrimSpace(n) != "" {
			c.Recipients = append(c.Recipients, n)
		}
	}
	if c.Gifter == "" || len(c.Recipients) == 0 {
		return c, errors.New("gifter and at least one recipient required")
	}
	if !c.Preview && (c.PreviewPNG != "" || c.PreviewPGM != "") {
		return c, errors.New("preview output requires --preview")
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
	line := strings.TrimSpace(string(o))
	const p = "system default destination: "
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
func build(c config) ([]byte, giftpage.Report, []byte, error) {
	img, rep, e := giftpage.Render(giftpage.Celebration{Total: c.Total, Gifter: c.Gifter, Recipients: c.Recipients, Missing: c.Missing})
	if e != nil {
		return nil, rep, nil, e
	}
	gray := append([]byte(nil), img.Pix...)
	intended, e := protocol.NativeMonochrome(gray, giftpage.Width, giftpage.Height)
	if e != nil {
		return nil, rep, nil, e
	}
	stream, e := protocol.EncodeNativeJob(gray, giftpage.Width, giftpage.Height, protocol.Settings{Speed: 12, Density: 9, Copies: 1})
	if e != nil {
		return nil, rep, nil, e
	}
	decoded, e := protocol.DecodeJob(stream)
	if e != nil {
		return nil, rep, nil, e
	}
	if decoded.WidthBytes != giftpage.Width/8 || decoded.Height != giftpage.Height || decoded.Copies != 1 || len(decoded.Rows) != len(intended) {
		return nil, rep, nil, errors.New("decoded geometry mismatch")
	}
	for i := range intended {
		if !bytes.Equal(intended[i], decoded.Rows[i]) {
			return nil, rep, nil, fmt.Errorf("decoded raster mismatch row %d", i+1)
		}
	}
	return stream, rep, gray, nil
}
func writePreviews(c config, gray []byte) error {
	if c.PreviewPNG != "" {
		img, _, e := giftpage.Render(giftpage.Celebration{Total: c.Total, Gifter: c.Gifter, Recipients: c.Recipients, Missing: c.Missing})
		if e != nil {
			return e
		}
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
	if c.PreviewPGM != "" {
		header := []byte(fmt.Sprintf("P5\n%d %d\n255\n", giftpage.Width, giftpage.Height))
		data := append(header, gray...)
		if e := os.WriteFile(c.PreviewPGM, data, 0600); e != nil {
			return e
		}
	}
	return nil
}
func run(args []string, env string, out io.Writer, r runner, s submitter) error {
	c, e := parseArgs(args, env)
	if e != nil {
		return e
	}
	stream, rep, gray, e := build(c)
	if e != nil {
		return e
	}
	if e = writePreviews(c, gray); e != nil {
		return e
	}
	sum := sha256.Sum256(stream)
	result := report{OK: true, Preview: c.Preview, Queue: c.Queue, Gifter: rep.Gifter, Total: rep.Total, Displayed: rep.Displayed, Missing: rep.Missing, More: rep.More, WidthDots: giftpage.Width, Rows: giftpage.Height, Bytes: len(stream), SHA256: hex.EncodeToString(sum[:])}
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
		title := "Twitch gift celebration: " + strconv.Itoa(c.Total)
		o, e := s("lp", stream, "-n", "1", "-d", c.Queue, "-o", "raw", "-t", title, "-")
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
func cmd(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	return c
}
func main() {
	if e := run(os.Args[1:], os.Getenv("F11_QUEUE"), os.Stdout, func(n string, a ...string) ([]byte, error) { return cmd(n, a...).CombinedOutput() }, func(n string, b []byte, a ...string) ([]byte, error) {
		c := cmd(n, a...)
		c.Stdin = bytes.NewReader(b)
		return c.CombinedOutput()
	}); e != nil {
		fmt.Fprintln(os.Stderr, "giftprint:", e)
		os.Exit(2)
	}
}
