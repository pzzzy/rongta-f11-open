package usb

import (
	"errors"
	"testing"
)

type fakeBackend struct {
	devices           []Device
	opened            string
	claimed, released bool
	writes            [][]byte
	failAt            int
}

func (f *fakeBackend) Discover() ([]Device, error) { return f.devices, nil }
func (f *fakeBackend) Open(path string) error      { f.opened = path; return nil }
func (f *fakeBackend) Claim(iface int) error       { f.claimed = iface == 0; return nil }
func (f *fakeBackend) Bulk(endpoint byte, data []byte, timeoutMS int) (int, error) {
	if f.failAt > 0 && len(f.writes)+1 == f.failAt {
		return 0, errors.New("usb failure")
	}
	f.writes = append(f.writes, append([]byte(nil), data...))
	return len(data), nil
}
func (f *fakeBackend) Release(iface int) error { f.released = iface == 0; return nil }
func (f *fakeBackend) Close() error            { return nil }

func TestProbeFindsExactF11(t *testing.T) {
	b := &fakeBackend{devices: []Device{{VID: 0x1234, PID: 0x811e}, {VID: 0x0fe6, PID: 0x811e, Path: "/dev/bus/usb/001/002", Serial: "A"}}}
	d, err := Probe(b)
	if err != nil || d.Serial != "A" {
		t.Fatalf("%+v %v", d, err)
	}
}
func TestProbeRejectsMissing(t *testing.T) {
	if _, err := Probe(&fakeBackend{}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
func TestSendClaimsChunksAndReleases(t *testing.T) {
	b := &fakeBackend{devices: []Device{{VID: F11VID, PID: F11PID, Path: "/dev/x"}}}
	stream := make([]byte, 10000)
	if err := Send(b, stream, 4096); err != nil {
		t.Fatal(err)
	}
	if !b.claimed || !b.released || b.opened != "/dev/x" || len(b.writes) != 3 {
		t.Fatalf("%+v", b)
	}
}
func TestSendRejectsEmptyOrHuge(t *testing.T) {
	b := &fakeBackend{}
	if err := Send(b, nil, 4096); err == nil {
		t.Fatal("empty")
	}
	if err := Send(b, make([]byte, MaxStreamBytes+1), 4096); err == nil {
		t.Fatal("huge")
	}
}
func TestSendPropagatesShortWrite(t *testing.T) {
	b := &fakeBackend{devices: []Device{{VID: F11VID, PID: F11PID, Path: "/dev/x"}}, failAt: 2}
	if err := Send(b, make([]byte, 9000), 4096); err == nil {
		t.Fatal("failure accepted")
	}
	if !b.released {
		t.Fatal("interface not released")
	}
}
