//go:build !linux

package usb

import "errors"

type unsupportedBackend struct{}

func NewUSBFS() Backend { return unsupportedBackend{} }
func (unsupportedBackend) Discover() ([]Device, error) {
	return nil, errors.New("USBFS is available only on Linux")
}
func (unsupportedBackend) Open(string) error { return errors.New("unsupported platform") }
func (unsupportedBackend) Claim(int) error   { return errors.New("unsupported platform") }
func (unsupportedBackend) Bulk(byte, []byte, int) (int, error) {
	return 0, errors.New("unsupported platform")
}
func (unsupportedBackend) Release(int) error { return nil }
func (unsupportedBackend) Close() error      { return nil }
