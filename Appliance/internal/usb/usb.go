package usb

import (
	"errors"
	"fmt"
)

const (
	F11VID         = 0x0fe6
	F11PID         = 0x811e
	MaxStreamBytes = 64 << 20
)

var ErrNotFound = errors.New("Rongta F11 not found")

type Device struct {
	VID, PID     uint16
	Path, Serial string
}
type Backend interface {
	Discover() ([]Device, error)
	Open(string) error
	Claim(int) error
	Bulk(byte, []byte, int) (int, error)
	Release(int) error
	Close() error
}

func Probe(b Backend) (Device, error) {
	devices, err := b.Discover()
	if err != nil {
		return Device{}, err
	}
	for _, d := range devices {
		if d.VID == F11VID && d.PID == F11PID && d.Path != "" {
			return d, nil
		}
	}
	return Device{}, ErrNotFound
}
func Send(b Backend, stream []byte, chunk int) error {
	if len(stream) == 0 || len(stream) > MaxStreamBytes {
		return errors.New("invalid stream size")
	}
	if chunk <= 0 || chunk > 1<<20 {
		return errors.New("invalid chunk size")
	}
	d, err := Probe(b)
	if err != nil {
		return err
	}
	if err = b.Open(d.Path); err != nil {
		return err
	}
	defer b.Close()
	if err = b.Claim(0); err != nil {
		return fmt.Errorf("claim interface: %w", err)
	}
	defer b.Release(0)
	for off := 0; off < len(stream); {
		end := off + chunk
		if end > len(stream) {
			end = len(stream)
		}
		n, e := b.Bulk(0x01, stream[off:end], 5000)
		if e != nil {
			return fmt.Errorf("bulk transfer at %d: %w", off, e)
		}
		if n != end-off {
			return fmt.Errorf("short USB write at %d: %d/%d", off, n, end-off)
		}
		off = end
	}
	return nil
}
