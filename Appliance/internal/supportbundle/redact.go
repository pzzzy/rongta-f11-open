// Package supportbundle provides deterministic privacy scrubbing primitives.
package supportbundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

const Redacted = "[REDACTED]"

var textRules = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?im)^(authorization\s*:\s*).*$`), `${1}` + Redacted},
	{regexp.MustCompile(`(?im)^((?:set-)?cookie\s*:\s*).*$`), `${1}` + Redacted},
	{regexp.MustCompile(`(?im)(journal_record\s*=\s*).*$`), `${1}` + Redacted},
	{regexp.MustCompile(`(?i)\b(event|gift|channel|user|text|message|gifter|viewer|job_id)\b(\s*[=:]\s*)("[^"]*"|'[^']*'|[^&\s]+)`), `${1}${2}` + Redacted},
	{regexp.MustCompile(`(?i)\b(access_token|refresh_token|oauth_token|client_secret|authorization_code|oauth_code|code|psk|wifi_password|ssid|setup_code)\b(\s*[=:]\s*)("[^"]*"|'[^']*'|[^&\s]+)`), `${1}${2}` + Redacted},
	{regexp.MustCompile(`(?i)\b(ID_SERIAL_SHORT|usb_serial|serial)\b(\s*[=:]\s*)("[^"]*"|'[^']*'|[^&\s]+)`), `${1}${2}` + Redacted},
	{regexp.MustCompile(`(?i)\b(ip|ip_address|remote)\b(\s*[=:]\s*)(\[[0-9a-f:]+\]|(?:\d{1,3}\.){3}\d{1,3}|[0-9a-f]*:[0-9a-f:]+)`), `${1}${2}` + Redacted},
	{regexp.MustCompile(`(?i)\b(mac|mac_address)\b(\s*[=:]\s*)([0-9a-f]{2}(?::[0-9a-f]{2}){5})`), `${1}${2}` + Redacted},
	{regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b`), Redacted},
	{regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`), Redacted},
	{regexp.MustCompile(`(?i)\[[0-9a-f:]*:[0-9a-f:]+\]`), Redacted},
	{regexp.MustCompile(`(?i)\b[0-9a-f]{1,4}(?::[0-9a-f]{0,4}){2,7}\b`), Redacted},
}

// RedactText scrubs common structured log and command-output representations.
func RedactText(input string) string {
	output := input
	for _, rule := range textRules {
		output = rule.re.ReplaceAllString(output, rule.repl)
	}
	return output
}

// RedactJSON parses one JSON value and recursively redacts values selected by
// a conservative key policy. Invalid or concatenated input is rejected.
func RedactJSON(input []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("trailing JSON value")
		}
		return nil, err
	}
	redactValue(value)
	return json.MarshalIndent(value, "", "  ")
}

func redactValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveKey(key) {
				typed[key] = Redacted
			} else {
				redactValue(child)
			}
		}
	case []any:
		for _, child := range typed {
			redactValue(child)
		}
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	exact := map[string]bool{
		"ssid": true, "psk": true, "wifi_password": true, "password": true, "setup_code": true,
		"access_token": true, "refresh_token": true, "client_secret": true,
		"authorization": true, "authorization_code": true, "oauth_code": true,
		"code": true, "cookie": true, "cookies": true, "set_cookie": true,
		"ip": true, "ip_address": true, "remote_ip": true, "mac": true,
		"mac_address": true, "serial": true, "usb_serial": true,
		"id_serial_short": true, "journal_record": true, "event_journal": true,
	}
	return exact[normalized] || strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_secret") || strings.HasSuffix(normalized, "_cookie")
}

// ScanCanaries returns, in caller-supplied order, canaries still present in
// data. Matching is case-insensitive; empty canaries are ignored.
func ScanCanaries(data []byte, canaries []string) []string {
	lower := strings.ToLower(string(data))
	var found []string
	for _, canary := range canaries {
		if canary != "" && strings.Contains(lower, strings.ToLower(canary)) {
			found = append(found, canary)
		}
	}
	return found
}
