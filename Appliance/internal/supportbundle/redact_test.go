package supportbundle

import (
	"strings"
	"testing"
)

func TestRedactTextRemovesSensitiveValues(t *testing.T) {
	input := strings.Join([]string{
		`Authorization: Bearer oauth:super-secret-token`,
		`Cookie: session=deadbeef; twitch=tokenvalue`,
		`Set-Cookie: setup=abc123; HttpOnly`,
		`client_secret=hunter2&code=oauth-code&access_token=oauth-access&refresh_token=oauth-refresh`,
		`oauth_token=oauth-prefixed wifi_password=another-wifi-secret`,
		`ssid="My Home WiFi" psk="correct horse battery staple" setup_code=123456`,
		`connected from 192.168.50.22 via 2001:db8::cafe interface aa:bb:cc:dd:ee:ff`,
		`serial=F11-ABCDEF123456 ID_SERIAL_SHORT=ABCDEF123456`,
		`journal_record={"event_id":"evt-secret","payload":"private"}`,
	}, "\n")

	got := RedactText(input)
	for _, secret := range []string{
		"super-secret-token", "deadbeef", "tokenvalue", "abc123", "hunter2",
		"oauth-code", "oauth-access", "oauth-refresh", "oauth-prefixed", "another-wifi-secret", "My Home WiFi",
		"correct horse battery staple", "123456", "192.168.50.22",
		"2001:db8::cafe", "aa:bb:cc:dd:ee:ff", "ABCDEF123456",
		"evt-secret", "private",
	} {
		if strings.Contains(got, secret) {
			t.Errorf("redacted output leaks %q:\n%s", secret, got)
		}
	}
	if !strings.Contains(got, Redacted) {
		t.Fatalf("output has no redaction markers: %s", got)
	}
}

func TestRedactTextPreservesSafeDiagnosticContext(t *testing.T) {
	input := "service=f11-setup status=healthy usb_vendor=0fe6 usb_product=811e retries=2"
	if got := RedactText(input); got != input {
		t.Fatalf("safe diagnostic changed:\n got %q\nwant %q", got, input)
	}
}

func TestRedactJSONRecursesAndUsesKeyPolicy(t *testing.T) {
	input := []byte(`{"status":"ok","network":{"ssid":"Cafe","psk":"wifi-pass","ip_address":"10.2.3.4"},"oauth":{"access_token":"tok","client_id":"safe-public-id"},"cookies":["one","two"],"usb_serial":"0123456789ABCDEF"}`)
	got, err := RedactJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, secret := range []string{"Cafe", "wifi-pass", "10.2.3.4", `"tok"`, `"one"`, `"two"`, "0123456789ABCDEF"} {
		if strings.Contains(text, secret) {
			t.Errorf("JSON leaks %q: %s", secret, text)
		}
	}
	for _, safe := range []string{`"status": "ok"`, `"client_id": "safe-public-id"`} {
		if !strings.Contains(text, safe) {
			t.Errorf("JSON lost safe value %q: %s", safe, text)
		}
	}
}

func TestRedactTextRemovesTwitchActivityAndJobIdentifiers(t *testing.T) {
	input := `event=evt123 gift=gift456 channel=privatechannel user=alice text="private cheer words" message='hello world' gifter=bob viewer=carol job_id=Rongta_F11_Media-99`
	got := RedactText(input)
	for _, secret := range []string{"evt123", "gift456", "privatechannel", "alice", "private cheer words", "hello world", "bob", "carol", "Rongta_F11_Media-99"} {
		if strings.Contains(got, secret) {
			t.Fatalf("activity leaked %q in %q", secret, got)
		}
	}
}

func TestRedactJSONRejectsInvalidAndTrailingInput(t *testing.T) {
	for _, input := range []string{`{"token":`, `{"status":"ok"} {}`} {
		if _, err := RedactJSON([]byte(input)); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}

func TestScanCanariesFindsLeaksCaseInsensitively(t *testing.T) {
	leaks := ScanCanaries([]byte("prefix OAUTH-SEcrET suffix wifi-Canary"), []string{"oauth-secret", "WIFI-canary", "absent"})
	if len(leaks) != 2 || leaks[0] != "oauth-secret" || leaks[1] != "WIFI-canary" {
		t.Fatalf("leaks = %#v", leaks)
	}
}

func TestTextRedactionIsDeterministicAndIdempotent(t *testing.T) {
	input := "Authorization: Basic dXNlcjpwYXNz\nssid=PrivateNet\n"
	first := RedactText(input)
	if second := RedactText(first); second != first {
		t.Fatalf("not idempotent:\nfirst %q\nsecond %q", first, second)
	}
	if other := RedactText(input); other != first {
		t.Fatalf("not deterministic: %q != %q", other, first)
	}
}
