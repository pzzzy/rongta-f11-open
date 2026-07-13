package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/banner"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/protocol"
)

const (
	bannerWidth  = 1664
	bannerRows   = 3045
	designWidth  = 3045
	designHeight = 1664
	bannerMargin = 45
	maxTextBytes = 256
)

type config struct {
	Queue     string
	LineCount int
	Font      banner.FontStyle
	Preview   bool
	Text      string
}

type report struct {
	OK        bool     `json:"ok"`
	Preview   bool     `json:"preview"`
	Queue     string   `json:"queue"`
	Lines     []string `json:"lines"`
	Font      string   `json:"font"`
	FontName  string   `json:"font_name"`
	FontSize  float64  `json:"font_size"`
	WidthDots int      `json:"width_dots"`
	Rows      int      `json:"rows"`
	Bytes     int      `json:"bytes"`
	SHA256    string   `json:"sha256"`
	Submitted bool     `json:"submitted"`
	JobID     string   `json:"job_id,omitempty"`
}

type commandRunner func(name string, args ...string) ([]byte, error)
type streamSubmitter func(name string, stream []byte, args ...string) ([]byte, error)

var queuePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,126}$`)
var jobPattern = regexp.MustCompile(`request id is ([A-Za-z0-9_.-]+-[0-9]+)\b`)

func parseArgs(args []string, envQueue string) (config, error) {
	queue := envQueue
	fs := flag.NewFlagSet("bannerprint", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	lines := fs.String("lines", "auto", "auto, 1, 2, or 3")
	fontName := fs.String("font", "bold", "bold or comic-sans")
	preview := fs.Bool("preview", false, "render and validate without printing")
	fs.StringVar(&queue, "queue", queue, "CUPS queue")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if queue != "" && !queuePattern.MatchString(queue) {
		return config{}, errors.New("invalid queue")
	}
	lineCount := 0
	switch *lines {
	case "auto":
	case "1":
		lineCount = 1
	case "2":
		lineCount = 2
	case "3":
		lineCount = 3
	default:
		return config{}, errors.New("lines must be auto, 1, 2, or 3")
	}
	style := banner.FontStyle(*fontName)
	if style != banner.FontGoBold && style != banner.FontComicSans {
		return config{}, errors.New("font must be bold or comic-sans")
	}
	rawText := strings.Join(fs.Args(), " ")
	if !utf8.ValidString(rawText) {
		return config{}, errors.New("text must be valid UTF-8")
	}
	for _, r := range rawText {
		if !unicode.IsPrint(r) || unicode.Is(unicode.M, r) {
			return config{}, errors.New("text contains unsupported control, format, or combining characters")
		}
	}
	text := strings.Join(strings.Fields(rawText), " ")
	if text == "" || len(text) > maxTextBytes || len(strings.Fields(text)) > 16 {
		return config{}, errors.New("text must contain 1 to 256 bytes and at most 16 words")
	}
	if !banner.SupportsText(style, text) {
		return config{}, errors.New("selected font does not support every character")
	}
	return config{Queue: queue, LineCount: lineCount, Font: style, Preview: *preview, Text: text}, nil
}

func resolveQueue(queue string, runner commandRunner) (string, error) {
	if queue != "" {
		if !queuePattern.MatchString(queue) || !strings.HasPrefix(queue, "Rongta_F11") {
			return "", errors.New("queue must be an F11 queue")
		}
		return queue, nil
	}
	output, err := runner("lpstat", "-d")
	if err != nil {
		return "", fmt.Errorf("cannot read CUPS default: %w", err)
	}
	const prefix = "system default destination: "
	line := strings.TrimSpace(string(output))
	if !strings.HasPrefix(line, prefix) {
		return "", errors.New("CUPS has no default destination")
	}
	queue = strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if !queuePattern.MatchString(queue) || !strings.HasPrefix(queue, "Rongta_F11") {
		return "", errors.New("CUPS default is not an F11 queue")
	}
	return queue, nil
}

func verifyQueue(queue string, runner commandRunner) error {
	line, err := runner("lpstat", "-v", queue)
	if err != nil {
		return fmt.Errorf("cannot inspect CUPS queue: %w", err)
	}
	if len(line) > 4096 || !strings.HasPrefix(string(line), "device for "+queue+": ") {
		return errors.New("unexpected CUPS queue identity")
	}
	verified, err := runner("/usr/local/lib/f11/check-f11-runtime", queue, strings.TrimSpace(string(line)), "/sys/bus/usb/devices")
	if err != nil || strings.TrimSpace(string(verified)) == "" {
		return errors.New("queue is not the attached F11 USB device")
	}
	return nil
}

func build(cfg config) ([]byte, banner.Layout, error) {
	layout, err := banner.PlanLines(cfg.Text, designWidth, designHeight, bannerMargin, cfg.LineCount, cfg.Font)
	if err != nil {
		return nil, banner.Layout{}, err
	}
	gray, err := banner.Render(layout)
	if err != nil {
		return nil, banner.Layout{}, err
	}
	intended, err := protocol.NativeMonochrome(gray, bannerWidth, bannerRows)
	if err != nil {
		return nil, banner.Layout{}, err
	}
	stream, err := protocol.EncodeNativeJob(gray, bannerWidth, bannerRows, protocol.Settings{Speed: 12, Density: 9, Copies: 1})
	if err != nil {
		return nil, banner.Layout{}, err
	}
	decoded, err := protocol.DecodeJob(stream)
	if err != nil {
		return nil, banner.Layout{}, err
	}
	if decoded.WidthBytes != bannerWidth/8 || decoded.Height != bannerRows || decoded.Copies != 1 || len(decoded.Rows) != len(intended) {
		return nil, banner.Layout{}, errors.New("decoded geometry mismatch")
	}
	for i := range intended {
		if !bytes.Equal(intended[i], decoded.Rows[i]) {
			return nil, banner.Layout{}, fmt.Errorf("decoded raster mismatch at row %d", i+1)
		}
	}
	return stream, layout, nil
}

func fontDisplayName(style banner.FontStyle) string {
	if style == banner.FontComicSans {
		return "Comic Neue Bold (Comic Sans style, SIL OFL 1.1)"
	}
	return "Go Bold"
}

func jobTitle(text string) string {
	const maxTitle = 80
	title := "bannerprint: "
	for _, r := range text {
		if len(title)+utf8.RuneLen(r) > maxTitle {
			break
		}
		title += string(r)
	}
	return title
}

func run(args []string, envQueue string, output io.Writer, runner commandRunner, submitter streamSubmitter) error {
	cfg, err := parseArgs(args, envQueue)
	if err != nil {
		return err
	}
	stream, layout, err := build(cfg)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(stream)
	r := report{
		OK: true, Preview: cfg.Preview, Queue: cfg.Queue, Lines: layout.Lines,
		Font: string(cfg.Font), FontName: fontDisplayName(cfg.Font), FontSize: layout.FontSize,
		WidthDots: bannerWidth, Rows: bannerRows, Bytes: len(stream), SHA256: hex.EncodeToString(sum[:]),
	}
	if cfg.Preview && r.Queue == "" {
		r.Queue = "auto"
	}
	if !cfg.Preview {
		cfg.Queue, err = resolveQueue(cfg.Queue, runner)
		if err != nil {
			return err
		}
		if err := verifyQueue(cfg.Queue, runner); err != nil {
			return err
		}
		r.Queue = cfg.Queue
		lpOutput, err := submitter("lp", stream, "-n", "1", "-d", cfg.Queue, "-o", "raw", "-t", jobTitle(cfg.Text), "-")
		if err != nil {
			return fmt.Errorf("CUPS submission status unknown; do not retry automatically: %w", err)
		}
		r.Submitted = true
		if match := jobPattern.FindSubmatch(lpOutput); match != nil {
			r.JobID = string(match[1])
		}
	}
	return json.NewEncoder(output).Encode(r)
}

func localeCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	return cmd
}

func realRunner(name string, args ...string) ([]byte, error) {
	return localeCommand(name, args...).CombinedOutput()
}

func realSubmitter(name string, stream []byte, args ...string) ([]byte, error) {
	cmd := localeCommand(name, args...)
	cmd.Stdin = bytes.NewReader(stream)
	return cmd.CombinedOutput()
}

func main() {
	if err := run(os.Args[1:], os.Getenv("F11_QUEUE"), os.Stdout, realRunner, realSubmitter); err != nil {
		fmt.Fprintln(os.Stderr, "bannerprint:", err)
		fmt.Fprintln(os.Stderr, "usage: bannerprint [--lines auto|1|2|3] [--font bold|comic-sans] [--preview] [--queue NAME] TEXT...")
		os.Exit(2)
	}
}
