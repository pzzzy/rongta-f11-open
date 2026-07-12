//go:build linux

package usb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	usbdevfsClaimInterface   = 0x8004550f
	usbdevfsReleaseInterface = 0x80045510
)

func bulkIOCTL() uintptr {
	// _IOWR('U', 2, struct usbdevfs_bulktransfer). The structure is 24 bytes
	// on arm64 and 16 bytes on 32-bit ARM because its final member is a pointer.
	return uintptr(0xc0000000 | (uint32(unsafe.Sizeof(bulkTransfer{})) << 16) | ('U' << 8) | 2)
}

type bulkTransfer struct {
	Endpoint uint32
	Length   uint32
	Timeout  uint32
	Data     uintptr
}
type USBFS struct {
	file  *os.File
	sysfs string
}

func NewUSBFS() *USBFS { return &USBFS{sysfs: "/sys/bus/usb/devices"} }
func readHex(path string) (uint16, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return 0, e
	}
	v, e := strconv.ParseUint(strings.TrimSpace(string(b)), 16, 16)
	return uint16(v), e
}
func readTrim(path string) string { b, _ := os.ReadFile(path); return strings.TrimSpace(string(b)) }
func (u *USBFS) Discover() ([]Device, error) {
	entries, e := os.ReadDir(u.sysfs)
	if e != nil {
		return nil, e
	}
	var out []Device
	for _, entry := range entries {
		base := filepath.Join(u.sysfs, entry.Name())
		vid, e1 := readHex(filepath.Join(base, "idVendor"))
		pid, e2 := readHex(filepath.Join(base, "idProduct"))
		if e1 != nil || e2 != nil {
			continue
		}
		bus, e1 := strconv.Atoi(readTrim(filepath.Join(base, "busnum")))
		dev, e2 := strconv.Atoi(readTrim(filepath.Join(base, "devnum")))
		if e1 != nil || e2 != nil {
			continue
		}
		out = append(out, Device{VID: vid, PID: pid, Path: fmt.Sprintf("/dev/bus/usb/%03d/%03d", bus, dev), Serial: readTrim(filepath.Join(base, "serial"))})
	}
	return out, nil
}
func (u *USBFS) Open(path string) error {
	f, e := os.OpenFile(path, os.O_RDWR, 0)
	if e != nil {
		return e
	}
	u.file = f
	return nil
}

var ioctlCall = func(fd uintptr, request uintptr, arg unsafe.Pointer) (uintptr, error) {
	r1, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(arg))
	if errno != 0 {
		return 0, errno
	}
	return r1, nil
}

func ioctl(fd uintptr, request uintptr, arg unsafe.Pointer) error {
	_, err := ioctlCall(fd, request, arg)
	return err
}
func (u *USBFS) Claim(iface int) error {
	if u.file == nil {
		return errors.New("USB device not open")
	}
	v := uint32(iface)
	return ioctl(u.file.Fd(), usbdevfsClaimInterface, unsafe.Pointer(&v))
}
func (u *USBFS) Release(iface int) error {
	if u.file == nil {
		return nil
	}
	v := uint32(iface)
	return ioctl(u.file.Fd(), usbdevfsReleaseInterface, unsafe.Pointer(&v))
}
func (u *USBFS) Bulk(endpoint byte, data []byte, timeoutMS int) (int, error) {
	if u.file == nil || len(data) == 0 {
		return 0, errors.New("invalid bulk transfer")
	}
	x := bulkTransfer{Endpoint: uint32(endpoint), Length: uint32(len(data)), Timeout: uint32(timeoutMS), Data: uintptr(unsafe.Pointer(&data[0]))}
	n, e := ioctlCall(u.file.Fd(), bulkIOCTL(), unsafe.Pointer(&x))
	runtime.KeepAlive(data)
	if e != nil {
		return 0, e
	}
	return int(n), nil
}
func (u *USBFS) Close() error {
	if u.file == nil {
		return nil
	}
	e := u.file.Close()
	u.file = nil
	return e
}
