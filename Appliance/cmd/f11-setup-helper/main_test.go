package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls  [][]string
	inputs [][]byte
	output map[string][]byte
	err    error
}

func (f *fakeRunner) Run(_ context.Context, argv []string, stdin []byte) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), argv...))
	f.inputs = append(f.inputs, append([]byte(nil), stdin...))
	if f.err != nil {
		return nil, f.err
	}
	if len(argv) == 1 && argv[0] == plannerPath {
		lines := strings.Split(strings.TrimSpace(string(stdin)), "\n")
		if len(lines) != 1 {
			return nil, errors.New("ambiguous")
		}
		return []byte("usb://Rongta/F11"), nil
	}
	return f.output[strings.Join(argv, "\x00")], nil
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	_, err := decodeRequest(strings.NewReader(`{"op":"facts","extra":1}`))
	if err == nil {
		t.Fatal("unknown field accepted")
	}
}
func TestValidateWiFiNeverIncludesPSKInError(t *testing.T) {
	p := "secretpass"
	err := validateWiFi("", p)
	if err == nil || strings.Contains(err.Error(), p) {
		t.Fatalf("err=%v", err)
	}
	if validateWiFi(strings.Repeat("x", 33), p) == nil {
		t.Fatal("long ssid accepted")
	}
	if validateWiFi("ssid", "short") == nil {
		t.Fatal("short psk accepted")
	}
}
func TestWiFiConnectUsesDirectArgvAndDoesNotReturnPSK(t *testing.T) {
	f := &fakeRunner{}
	s := &server{runner: f}
	r := request{Op: "wifi_connect", SSID: "my wifi", PSK: "password123"}
	resp := s.handle(context.Background(), r)
	if !resp.OK {
		t.Fatal(resp.Error)
	}
	if len(f.calls) != 1 || f.calls[0][0] != "/usr/bin/nmcli" {
		t.Fatalf("calls=%v", f.calls)
	}
	if strings.Contains(respJSON(resp), r.PSK) {
		t.Fatal("psk in response")
	}
}
func TestWiFiStatusAcceptsStationAndRejectsRecoveryAP(t *testing.T) {
	d := t.TempDir()
	wifiStatusEvidencePath = filepath.Join(d, "wifi-status.json")
	t.Cleanup(func() { wifiStatusEvidencePath = "/var/lib/f11-setup/wifi-status-evidence.json" })
	stateKey := "/usr/bin/nmcli\x00-g\x00GENERAL.STATE\x00device\x00show\x00wlan0"
	connectionKey := "/usr/bin/nmcli\x00-g\x00GENERAL.CONNECTION\x00device\x00show\x00wlan0"
	f := &fakeRunner{output: map[string][]byte{stateKey: []byte("100 (connected)\n"), connectionKey: []byte("f11-home\n")}}
	s := &server{runner: f}
	good := s.handle(context.Background(), request{Op: "wifi_status"})
	if !good.OK || good.Data["connected"] != true || good.Data["recovery_ap"] != false {
		t.Fatalf("station status=%+v", good)
	}
	evidence, err := os.ReadFile(wifiStatusEvidencePath)
	if err != nil || strings.Contains(string(evidence), "f11-home") || !strings.Contains(string(evidence), `"state_connected":true`) {
		t.Fatalf("unsafe evidence=%q err=%v", evidence, err)
	}
	f.output[connectionKey] = []byte("f11-setup-ap\n")
	bad := s.handle(context.Background(), request{Op: "wifi_status"})
	if bad.OK {
		t.Fatalf("recovery AP accepted: %+v", bad)
	}
}
func TestPrinterProbeRefusesAmbiguityAndMasksSerial(t *testing.T) {
	out := "direct usb://Rongta/F11 usb Printer \"M1\" SERIAL:ABC123;MODEL:F11\n"
	f := &fakeRunner{output: map[string][]byte{backendKey(): []byte(out)}}
	resp := (&server{runner: f}).handle(context.Background(), request{Op: "printer_probe"})
	if !resp.OK {
		t.Fatal(resp.Error)
	}
	if resp.Printer.SerialSuffix != "***123" || resp.Printer.Model != "M1" {
		t.Fatalf("printer=%+v", resp.Printer)
	}
	f.output[backendKey()] = []byte(out + out)
	resp = (&server{runner: f}).handle(context.Background(), request{Op: "printer_probe"})
	if resp.OK || resp.Error.Code != "ambiguous_printer" {
		t.Fatalf("resp=%+v", resp)
	}
}
func TestPrinterConfigureCanonicalOnly(t *testing.T) {
	f := &fakeRunner{}
	resp := (&server{runner: f}).handle(context.Background(), request{Op: "printer_configure", Model: "other"})
	if resp.OK || resp.Error.Code != "invalid_model" {
		t.Fatalf("resp=%+v", resp)
	}
	resp = (&server{runner: f}).handle(context.Background(), request{Op: "printer_configure", Model: "Rongta_F11_Media"})
	if !resp.OK || len(f.calls) != 1 || f.calls[0][0] != "/usr/local/lib/f11/provision-printer" {
		t.Fatalf("resp=%+v calls=%v", resp, f.calls)
	}
}
func TestServiceStatusRequiresActiveEventSubReadiness(t *testing.T) {
	f := &fakeRunner{output: map[string][]byte{"/bin/systemctl\x00is-active\x00twitch-banner.service": []byte("active\n"), "/usr/local/lib/f11-image/verify-eventsub": []byte("ready\n")}}
	r := (&server{runner: f}).handle(context.Background(), request{Op: "service_status"})
	if !r.OK || r.Data["eventsub_ready"] != true || len(f.calls) != 2 {
		t.Fatalf("resp=%+v calls=%v", r, f.calls)
	}
	f.err = errors.New("not ready")
	bad := (&server{runner: f}).handle(context.Background(), request{Op: "service_status"})
	if bad.OK {
		t.Fatalf("failed verifier accepted: %+v", bad)
	}
}

func TestRecoveryAndServiceOperationsUseFixedTargets(t *testing.T) {
	f := &fakeRunner{}
	s := &server{runner: f}
	if r := s.handle(context.Background(), request{Op: "setup_ap"}); !r.OK {
		t.Fatal(r.Error)
	}
	if r := s.handle(context.Background(), request{Op: "service_restart"}); !r.OK {
		t.Fatal(r.Error)
	}
	if len(f.calls) != 2 || f.calls[0][0] != "/usr/local/lib/f11-image/network-recover" || strings.Join(f.calls[1], " ") != "/bin/systemctl restart twitch-banner.service" {
		t.Fatalf("calls=%v", f.calls)
	}
}

func TestPreviewAndPhysicalTestsUseFixedCommands(t *testing.T) {
	oldAttemptPath := physicalAttemptPath
	physicalAttemptPath = filepath.Join(t.TempDir(), "physical-test-attempted")
	t.Cleanup(func() { physicalAttemptPath = oldAttemptPath })
	f := &fakeRunner{output: map[string][]byte{
		"/usr/bin/runuser\x00-u\x00twitch-banner\x00--\x00/usr/local/bin/bannerprint\x00--preview\x00/tmp/f11-setup-banner.png\x00*":                                                                                                []byte(`{"ok":true,"rows":735}`),
		"/usr/bin/runuser\x00-u\x00twitch-banner\x00--\x00/usr/local/bin/raidprint\x00--preview\x00/tmp/f11-setup-raid.png\x00SetupRaid\x0047":                                                                                      []byte(`{"ok":true,"width_dots":1664,"rows":2233}`),
		"/usr/bin/runuser\x00-u\x00twitch-banner\x00--\x00/usr/local/bin/giftprint\x00--preview\x00/tmp/f11-setup-gift.png\x00SetupGifter\x0010\x00Alice\x00Bob\x00Carol\x00Dave\x00Eve\x00Frank\x00Grace\x00Heidi\x00Ivan\x00Judy": []byte(`{"ok":true,"width_dots":1664,"rows":2233}`),
		"/usr/bin/runuser\x00-u\x00twitch-banner\x00--\x00/usr/local/bin/bannerprint\x00*":                                                                                                                                          []byte(`{"ok":true,"submitted":true,"job_id":"Rongta_F11_Media-9"}`),
	}}
	s := &server{runner: f}
	if r := s.handle(context.Background(), request{Op: "preview_test"}); !r.OK {
		t.Fatalf("preview=%+v calls=%v", r, f.calls)
	}
	if r := s.handle(context.Background(), request{Op: "physical_test"}); !r.OK || r.Data["job_id"] != "Rongta_F11_Media-9" {
		t.Fatalf("physical=%+v", r)
	}
	calls := len(f.calls)
	if r := s.handle(context.Background(), request{Op: "physical_test"}); r.OK || r.Error.Code != "physical_test_already_attempted" || len(f.calls) != calls {
		t.Fatalf("repeat=%+v calls=%v", r, f.calls)
	}
}

func TestTwitchInstallUsesFixedHelperAndDoesNotEchoTokens(t *testing.T) {
	f := &fakeRunner{}
	r := request{Op: "twitch_install", ClientID: "publicclient123", Login: "channel", UserID: "12345", AccessToken: "access-secret", RefreshToken: "refresh-secret", ExpiresAt: "2027-01-01T00:00:00Z", Scopes: []string{"bits:read", "user:read:chat"}}
	resp := (&server{runner: f}).handle(context.Background(), r)
	if !resp.OK || len(f.calls) != 1 || len(f.calls[0]) != 1 || f.calls[0][0] != "/usr/local/lib/f11-image/install-twitch-authorization" {
		t.Fatalf("resp=%+v calls=%v", resp, f.calls)
	}
	if strings.Contains(respJSON(resp), "secret") || strings.Contains(strings.Join(f.calls[0], " "), "secret") || !strings.Contains(string(f.inputs[0]), "access-secret") {
		t.Fatalf("unsafe transport response=%+v calls=%v", resp, f.calls)
	}
	r.Scopes = []string{"bits:read"}
	if bad := (&server{runner: f}).handle(context.Background(), r); bad.OK || bad.Error.Code != "invalid_twitch_authorization" {
		t.Fatalf("invalid scopes accepted: %+v", bad)
	}
}

func TestDeadlineErrorsAreStructured(t *testing.T) {
	f := &fakeRunner{err: context.DeadlineExceeded}
	resp := (&server{runner: f, timeout: time.Nanosecond}).handle(context.Background(), request{Op: "facts"})
	if resp.OK || resp.Error.Code != "operation_timeout" {
		t.Fatalf("resp=%+v", resp)
	}
	_ = errors.New
}
func respJSON(r response) string { return r.ErrorMessage() + r.Printer.Model + r.Printer.SerialSuffix }
func (r response) ErrorMessage() string {
	if r.Error == nil {
		return ""
	}
	return r.Error.Message
}
