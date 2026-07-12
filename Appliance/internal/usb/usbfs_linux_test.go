//go:build linux

package usb

import (
	"os"
	"testing"
	"unsafe"
)

func TestBulkReturnsKernelTransferCount(t *testing.T) {
	old := ioctlCall
	defer func() { ioctlCall = old }()
	ioctlCall = func(fd, request uintptr, arg unsafe.Pointer) (uintptr, error) { return 7, nil }
	f, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	u := &USBFS{file: f}
	n, err := u.Bulk(1, make([]byte, 16), 1000)
	if err != nil || n != 7 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestBulkIOCTLMatchesArchitecture(t *testing.T) {
	size := unsafe.Sizeof(bulkTransfer{})
	want := uintptr(0xc0000000 | (uint32(size) << 16) | ('U' << 8) | 2)
	if bulkIOCTL() != want {
		t.Fatalf("ioctl=%x want=%x size=%d", bulkIOCTL(), want, size)
	}
	if size != 16 && size != 24 {
		t.Fatalf("unexpected bulk struct size %d", size)
	}
}
