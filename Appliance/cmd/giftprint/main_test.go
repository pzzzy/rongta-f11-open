package main

import (
	"bytes"
	"reflect"
	"testing"
)

func args() []string {
	return []string{"--preview", "--gifter", "Gift Hero", "--total", "10", "--recipients", "Alice,Bob,Carol,Dave,Eve,Frank,Grace,Heidi,Ivan,Judy"}
}
func TestPreviewBuildsValidatedLetterJob(t *testing.T) {
	var out bytes.Buffer
	if err := run(args(), "", &out, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"width_dots":1664`)) || !bytes.Contains(out.Bytes(), []byte(`"rows":2233`)) {
		t.Fatal(out.String())
	}
}
func TestSubmissionIsExactlyOneRawJob(t *testing.T) {
	var name string
	var got []string
	s := func(n string, b []byte, a ...string) ([]byte, error) {
		name = n
		got = append([]string(nil), a...)
		if len(b) == 0 {
			t.Fatal("empty stream")
		}
		return []byte("request id is Rongta_F11_Media-11 (1 file(s))\n"), nil
	}
	r := func(n string, a ...string) ([]byte, error) {
		if n == "lpstat" && reflect.DeepEqual(a, []string{"-v", "Rongta_F11_Media"}) {
			return []byte("device for Rongta_F11_Media: usb:///F11?serial=test\n"), nil
		}
		if n == "/usr/local/lib/f11/check-f11-runtime" {
			return []byte("ok"), nil
		}
		t.Fatalf("unexpected %s %#v", n, a)
		return nil, nil
	}
	a := args()[1:]
	a = append([]string{"--queue", "Rongta_F11_Media"}, a...)
	var out bytes.Buffer
	if err := run(a, "", &out, r, s); err != nil {
		t.Fatal(err)
	}
	want := []string{"-n", "1", "-d", "Rongta_F11_Media", "-o", "raw", "-t", "Twitch gift celebration: 10", "-"}
	if name != "lp" || !reflect.DeepEqual(got, want) {
		t.Fatalf("%s %#v", name, got)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"job_id":"Rongta_F11_Media-11"`)) {
		t.Fatal(out.String())
	}
}
func TestRejectsUnderTen(t *testing.T) {
	a := []string{"--preview", "--gifter", "G", "--total", "9", "--recipients", "A"}
	if _, err := parseArgs(a, ""); err == nil {
		t.Fatal("accepted")
	}
}
