package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	out  []byte
	err  error
	argv []string
}

func (f *fakeRunner) CombinedOutput(_ context.Context, a []string) ([]byte, error) {
	f.argv = append([]string(nil), a...)
	return f.out, f.err
}
func TestCollectRedactsAndAnnotatesFailure(t *testing.T) {
	f := &fakeRunner{out: []byte("ssid=Home\naccess_token=secret\nip=192.168.1.2\n"), err: errors.New("failed")}
	got := string(collect(context.Background(), f, check{"x", []string{"safe", "arg"}}))
	for _, secret := range []string{"Home", "secret", "192.168.1.2"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret %q leaked in %q", secret, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") || !strings.Contains(got, "command_status=failed") {
		t.Fatalf("missing redaction/status: %q", got)
	}
	if strings.Join(f.argv, " ") != "safe arg" {
		t.Fatalf("argv=%v", f.argv)
	}
}
func TestCollectCapsOutput(t *testing.T) {
	f := &fakeRunner{out: []byte(strings.Repeat("x", 600<<10))}
	got := collect(context.Background(), f, check{"x", []string{"safe"}})
	if len(got) != (512 << 10) {
		t.Fatalf("len=%d", len(got))
	}
}
func TestCheckNamesAreUniqueAndFlat(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range checks {
		if seen[c.name] || strings.ContainsAny(c.name, "/\\") {
			t.Fatalf("unsafe name %q", c.name)
		}
		seen[c.name] = true
	}
}
