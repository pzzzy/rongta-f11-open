package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	socketPath      = "/run/f11-setup/helper.sock"
	backendPath     = "/usr/lib/cups/backend/usb"
	plannerPath     = "/usr/local/lib/f11/plan-queue-migration"
	provisionPath   = "/usr/local/lib/f11/provision-printer"
	maxRequestBytes = 32 << 10
)

var physicalAttemptPath = "/var/lib/f11-setup/physical-test-attempted"
var wifiStatusEvidencePath = "/var/lib/f11-setup/wifi-status-evidence.json"

func writeWiFiStatusEvidence(commandOK, stateConnected, recoveryAP bool, fields int) {
	b, err := json.Marshal(map[string]any{
		"command_ok": commandOK, "state_connected": stateConnected,
		"recovery_ap": recoveryAP, "field_count": fields,
	})
	if err == nil {
		_ = os.WriteFile(wifiStatusEvidencePath, append(b, '\n'), 0o600)
	}
}

type request struct {
	Op           string   `json:"op"`
	SSID         string   `json:"ssid,omitempty"`
	PSK          string   `json:"psk,omitempty"`
	Model        string   `json:"model,omitempty"`
	ClientID     string   `json:"client_id,omitempty"`
	Login        string   `json:"login,omitempty"`
	UserID       string   `json:"user_id,omitempty"`
	AccessToken  string   `json:"access_token,omitempty"`
	RefreshToken string   `json:"refresh_token,omitempty"`
	ExpiresAt    string   `json:"expires_at,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}
type printer struct {
	Model        string `json:"model"`
	Present      bool   `json:"present"`
	SerialSuffix string `json:"serial_suffix,omitempty"`
}
type apiError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}
type response struct {
	OK      bool           `json:"ok"`
	Error   *apiError      `json:"error,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
	Printer printer        `json:"printer,omitempty"`
}

type runner interface {
	Run(context.Context, []string, []byte) ([]byte, error)
}
type execRunner struct{}

func (execRunner) Run(ctx context.Context, argv []string, stdin []byte) ([]byte, error) {
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if len(stdin) > 0 {
		c.Stdin = strings.NewReader(string(stdin))
	}
	return c.Output()
}

type server struct {
	runner  runner
	timeout time.Duration
}

func (s *server) handle(parent context.Context, r request) response {
	if s.timeout == 0 {
		s.timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	ok := func(data map[string]any) response { return response{OK: true, Data: data} }
	fail := func(code, msg, rem string) response {
		return response{Error: &apiError{Code: code, Message: msg, Remediation: rem}}
	}
	switch r.Op {
	case "facts":
		o, e := s.run(ctx, []string{"/usr/bin/uname", "-a"}, nil)
		if e != nil {
			return s.err(e, fail)
		}
		return ok(map[string]any{"system": strings.TrimSpace(string(o))})
	case "wifi_scan":
		o, e := s.run(ctx, []string{"/usr/bin/nmcli", "-t", "-f", "SSID,SECURITY,SIGNAL", "device", "wifi", "list"}, nil)
		if e != nil {
			return s.err(e, fail)
		}
		return ok(map[string]any{"networks": strings.Split(strings.TrimSpace(string(o)), "\n")})
	case "wifi_status":
		stateOut, e := s.run(ctx, []string{"/usr/bin/nmcli", "-g", "GENERAL.STATE", "device", "show", "wlan0"}, nil)
		if e != nil {
			writeWiFiStatusEvidence(false, false, false, 0)
			return s.err(e, fail)
		}
		connectionOut, e := s.run(ctx, []string{"/usr/bin/nmcli", "-g", "GENERAL.CONNECTION", "device", "show", "wlan0"}, nil)
		if e != nil {
			writeWiFiStatusEvidence(false, false, false, 1)
			return s.err(e, fail)
		}
		state := strings.TrimSpace(string(stateOut))
		connection := strings.TrimSpace(string(connectionOut))
		connected := strings.HasPrefix(state, "100")
		recovery := connection == "f11-setup-ap"
		writeWiFiStatusEvidence(true, connected, recovery, 2)
		if !connected || connection == "" || connection == "--" || recovery {
			return fail("station_wifi_not_connected", "The appliance is not connected to home Wi-Fi.", "Enter home Wi-Fi details or reconnect the appliance, then retry.")
		}
		return ok(map[string]any{"connected": true, "recovery_ap": false})
	case "wifi_connect":
		if e := validateWiFi(r.SSID, r.PSK); e != nil {
			return fail("invalid_wifi", e.Error(), "Use an SSID of 1–32 bytes and a WPA passphrase of 8–63 characters.")
		}
		o, e := s.run(ctx, []string{"/usr/bin/nmcli", "device", "wifi", "connect", r.SSID, "password", r.PSK}, nil)
		_ = o
		if e != nil {
			return s.err(e, fail)
		}
		return ok(map[string]any{"connected": true})
	case "setup_ap":
		o, e := s.run(ctx, []string{"/usr/local/lib/f11-image/network-recover"}, nil)
		_ = o
		if e != nil {
			return s.err(e, fail)
		}
		return ok(map[string]any{"ap": "ready"})
	case "printer_probe":
		return s.probe(ctx, fail)
	case "printer_configure":
		if r.Model != "Rongta_F11_Media" {
			return fail("invalid_model", "Only the canonical Rongta F11 queue can be configured.", "Select Rongta_F11_Media.")
		}
		_, e := s.run(ctx, []string{provisionPath, "Rongta_F11_Media"}, nil)
		if e != nil {
			return s.err(e, fail)
		}
		return ok(map[string]any{"configured": true, "queue": "Rongta_F11_Media"})
	case "service_status", "service_restart":
		verb := "is-active"
		if r.Op == "service_restart" {
			verb = "restart"
		}
		o, e := s.run(ctx, []string{"/bin/systemctl", verb, "twitch-banner.service"}, nil)
		if e != nil {
			return s.err(e, fail)
		}
		if r.Op == "service_status" {
			if _, e = s.run(ctx, []string{"/usr/local/lib/f11-image/verify-eventsub"}, nil); e != nil {
				return fail("eventsub_not_ready", "Twitch is connected but required subscriptions are not ready.", "Wait a few seconds and retry; export diagnostics if this persists.")
			}
		}
		return ok(map[string]any{"service": "twitch-banner", "status": strings.TrimSpace(string(o)), "eventsub_ready": r.Op == "service_status"})
	case "preview_test":
		commands := [][]string{
			{"/usr/bin/runuser", "-u", "twitch-banner", "--", "/usr/local/bin/bannerprint", "--preview", "/tmp/f11-setup-banner.png", "*"},
			{"/usr/bin/runuser", "-u", "twitch-banner", "--", "/usr/local/bin/raidprint", "--preview", "/tmp/f11-setup-raid.png", "SetupRaid", "47"},
			{"/usr/bin/runuser", "-u", "twitch-banner", "--", "/usr/local/bin/giftprint", "--preview", "/tmp/f11-setup-gift.png", "SetupGifter", "10", "Alice", "Bob", "Carol", "Dave", "Eve", "Frank", "Grace", "Heidi", "Ivan", "Judy"},
		}
		for _, command := range commands {
			o, e := s.run(ctx, command, nil)
			if e != nil || !validPreviewReport(o) {
				return fail("preview_failed", "A no-paper preview failed.", "Check installed renderer health and retry.")
			}
		}
		return ok(map[string]any{"previews": true})
	case "physical_test":
		if e := reservePhysicalAttempt(physicalAttemptPath); e != nil {
			return fail("physical_test_already_attempted", "A physical test was already attempted.", "Inspect the printer and CUPS; do not automatically retry an uncertain submission.")
		}
		o, e := s.run(ctx, []string{"/usr/bin/runuser", "-u", "twitch-banner", "--", "/usr/local/bin/bannerprint", "*"}, nil)
		if e != nil {
			return s.err(e, fail)
		}
		var report struct {
			OK, Submitted bool
			JobID         string `json:"job_id"`
		}
		if json.Unmarshal(o, &report) != nil || !report.OK || !report.Submitted || !regexp.MustCompile(`^Rongta_F11_Media-[0-9]+$`).MatchString(report.JobID) {
			return fail("print_failed", "The controlled print was not accepted.", "Check CUPS and printer health, then retry once.")
		}
		return ok(map[string]any{"job_id": report.JobID})
	case "twitch_install":
		if e := validateTwitchInstall(r); e != nil {
			return fail("invalid_twitch_authorization", "Twitch authorization was invalid.", "Restart Twitch authorization and approve the requested scopes.")
		}
		payload, e := json.Marshal(r)
		if e != nil {
			return s.err(e, fail)
		}
		_, e = s.run(ctx, []string{"/usr/local/lib/f11-image/install-twitch-authorization"}, append(payload, '\n'))
		if e != nil {
			return s.err(e, fail)
		}
		return ok(map[string]any{"installed": true, "service": "twitch-banner"})
	case "reboot":
		_, e := s.run(ctx, []string{"/bin/systemctl", "reboot"}, nil)
		if e != nil {
			return s.err(e, fail)
		}
		return ok(map[string]any{"rebooting": true})
	default:
		return fail("unsupported_operation", "Unsupported setup operation.", "Choose one of the documented setup operations.")
	}
}
func (s *server) run(ctx context.Context, a []string, in []byte) ([]byte, error) {
	return s.runner.Run(ctx, a, in)
}

func reservePhysicalAttempt(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(f, "%s\n", time.Now().UTC().Format(time.RFC3339)); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (s *server) err(e error, f func(string, string, string) response) response {
	if errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) {
		return f("operation_timeout", "The operation timed out.", "Retry the operation; check the appliance network or service state.")
	}
	return f("operation_failed", "The requested operation could not be completed.", "Check appliance health and retry.")
}
func validateWiFi(ssid, psk string) error {
	if n := len([]byte(ssid)); n < 1 || n > 32 {
		return errors.New("SSID length is invalid")
	}
	if n := len(psk); n < 8 || n > 63 {
		return errors.New("WPA passphrase length is invalid")
	}
	return nil
}

func validPreviewReport(data []byte) bool {
	var report struct {
		OK        bool `json:"ok"`
		WidthDots int  `json:"width_dots"`
		Rows      int  `json:"rows"`
	}
	if json.Unmarshal(data, &report) != nil || !report.OK || report.Rows < 1 || report.Rows > 4060 {
		return false
	}
	return report.WidthDots == 0 || report.WidthDots == 1664
}

func validateTwitchInstall(r request) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`).MatchString(r.ClientID) ||
		!regexp.MustCompile(`^[a-z0-9_]{1,25}$`).MatchString(r.Login) ||
		!regexp.MustCompile(`^[0-9]{1,20}$`).MatchString(r.UserID) ||
		r.AccessToken == "" || r.RefreshToken == "" {
		return errors.New("invalid Twitch fields")
	}
	if len(r.Scopes) != 2 || !((r.Scopes[0] == "bits:read" && r.Scopes[1] == "user:read:chat") || (r.Scopes[1] == "bits:read" && r.Scopes[0] == "user:read:chat")) {
		return errors.New("invalid Twitch scopes")
	}
	if _, err := time.Parse(time.RFC3339, r.ExpiresAt); err != nil {
		return errors.New("invalid Twitch expiry")
	}
	return nil
}
func backendKey() string { return backendPath }
func (s *server) probe(ctx context.Context, fail func(string, string, string) response) response {
	o, e := s.run(ctx, []string{backendPath}, nil)
	if e != nil {
		return s.err(e, fail)
	}
	planned, e := s.run(ctx, []string{plannerPath}, o)
	if e != nil {
		return fail("ambiguous_printer", "Exactly one Rongta F11 printer must be present.", "Connect one Rongta F11 printer and retry.")
	}
	uri := strings.TrimSpace(string(planned))
	lines := strings.Split(strings.TrimSpace(string(o)), "\n")
	var found []printer
	for _, line := range lines {
		if !strings.Contains(line, uri) {
			continue
		}
		p, ok := parseUSBLine(line)
		if ok {
			found = append(found, p)
		}
	}
	if len(found) != 1 {
		return fail("ambiguous_printer", "Exactly one Rongta F11 printer must be present.", "Connect one Rongta F11 printer and retry.")
	}
	return response{OK: true, Printer: found[0]}
}
func parseUSBLine(line string) (printer, bool) {
	if !strings.HasPrefix(line, "direct usb:") {
		return printer{}, false
	}
	upper := strings.ToUpper(line)
	if !strings.Contains(upper, "F11") {
		return printer{}, false
	}
	model := "F11"
	if i := strings.Index(line, "\""); i >= 0 {
		if j := strings.Index(line[i+1:], "\""); j >= 0 {
			model = line[i+1 : i+1+j]
		}
	}
	serial := ""
	for _, x := range strings.FieldsFunc(line, func(r rune) bool { return r == ';' || r == ' ' }) {
		if strings.HasPrefix(strings.ToUpper(x), "SERIAL:") {
			serial = strings.TrimSpace(strings.SplitN(x, ":", 2)[1])
		}
	}
	suffix := ""
	if serial != "" {
		if len(serial) > 3 {
			suffix = "***" + serial[len(serial)-3:]
		} else {
			suffix = "***"
		}
	}
	return printer{Model: model, Present: true, SerialSuffix: suffix}, true
}

func decodeRequest(r io.Reader) (request, error) {
	var req request
	d := json.NewDecoder(io.LimitReader(r, maxRequestBytes+1))
	d.DisallowUnknownFields()
	if e := d.Decode(&req); e != nil {
		return req, e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return req, errors.New("request must contain one JSON object")
	}
	return req, nil
}
func serve(c net.Conn, s *server) {
	defer c.Close()
	req, e := decodeRequest(c)
	if e != nil {
		json.NewEncoder(c).Encode(response{Error: &apiError{Code: "invalid_request", Message: "Invalid setup request.", Remediation: "Send one documented JSON request no larger than 32 KiB."}})
		return
	}
	json.NewEncoder(c).Encode(s.handle(context.Background(), req))
}
func main() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "must run as root")
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0750); err != nil {
		panic(err)
	}
	_ = os.Remove(socketPath)
	l, e := net.Listen("unix", socketPath)
	if e != nil {
		panic(e)
	}
	defer l.Close()
	gid, err := lookupGroup("f11-setup")
	if err != nil {
		panic(err)
	}
	if err = os.Chmod(socketPath, 0660); err != nil {
		panic(err)
	}
	if err = os.Chown(socketPath, 0, gid); err != nil {
		panic(err)
	}
	s := &server{runner: execRunner{}}
	for {
		c, e := l.Accept()
		if e != nil {
			continue
		}
		go serve(c, s)
	}
}
func lookupGroup(name string) (int, error) {
	b, e := os.ReadFile("/etc/group")
	if e != nil {
		return 0, e
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) > 2 && f[0] == name {
			var n int
			fmt.Sscan(f[2], &n)
			if n <= 0 {
				return 0, errors.New("invalid setup group ID")
			}
			return n, nil
		}
	}
	return 0, errors.New("required f11-setup group not found")
}

var _ = syscall.EINVAL
