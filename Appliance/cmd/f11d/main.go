package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/banner"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/protocol"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/render"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/usb"
)

type result struct {
	OK      bool   `json:"ok"`
	Command string `json:"command"`
	Detail  any    `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
}

func emit(v result, code int) { _ = json.NewEncoder(os.Stdout).Encode(v); os.Exit(code) }
func selfTest() result {
	const w, h = 1592, 8
	gray := bytes.Repeat([]byte{255}, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if (x+y)%17 == 0 {
				gray[y*w+x] = 0
			}
		}
	}
	stream, e := protocol.EncodeJob(gray, w, h, protocol.Settings{Speed: 16, Density: 8, Copies: 1})
	if e != nil {
		return result{Error: e.Error()}
	}
	d, e := protocol.DecodeJob(stream)
	if e != nil || d.Height != h {
		return result{Error: "independent decode failed"}
	}
	sum := sha256.Sum256(stream)
	return result{OK: true, Command: "self-test", Detail: map[string]any{"bytes": len(stream), "rows": d.Height, "sha256": hex.EncodeToString(sum[:])}}
}
func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		emit(result{Error: "usage: f11d self-test|probe|banner|encode-pgm|validate|send|diagnose"}, 2)
	}
	switch args[0] {
	case "self-test":
		r := selfTest()
		if !r.OK {
			emit(r, 1)
		}
		emit(r, 0)
	case "probe":
		b := usb.NewUSBFS()
		d, e := usb.Probe(b)
		if e != nil {
			emit(result{Command: "probe", Error: e.Error()}, 1)
		}
		emit(result{OK: true, Command: "probe", Detail: d}, 0)
	case "banner":
		if len(args) < 3 {
			emit(result{Error: "banner output.f11 TEXT..."}, 2)
		}
		layout, e := banner.Plan(strings.Join(args[2:], " "), 3045, 1664, 45)
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		gray, e := banner.Render(layout)
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		intended, e := protocol.NativeMonochrome(gray, 1664, 3045)
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		stream, e := protocol.EncodeNativeJob(gray, 1664, 3045, protocol.Settings{Speed: 12, Density: 9, Copies: 1})
		decoded, decodeErr := protocol.DecodeJob(stream)
		if e == nil {
			e = decodeErr
		}
		if e == nil && (decoded.WidthBytes != 208 || decoded.Height != 3045 || len(decoded.Rows) != len(intended)) {
			e = fmt.Errorf("decoded geometry mismatch")
		}
		if e == nil {
			for i := range intended {
				if !bytes.Equal(intended[i], decoded.Rows[i]) {
					e = fmt.Errorf("raster mismatch row %d", i+1)
					break
				}
			}
		}
		if e == nil {
			e = os.WriteFile(args[1], stream, 0600)
		}
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		sum := sha256.Sum256(stream)
		emit(result{OK: true, Command: "banner", Detail: map[string]any{"output": filepath.Base(args[1]), "lines": layout.Lines, "font_size": layout.FontSize, "bytes": len(stream), "sha256": hex.EncodeToString(sum[:])}}, 0)
	case "encode-pgm":
		if len(args) != 3 {
			emit(result{Error: "encode-pgm input.pgm output.f11"}, 2)
		}
		data, e := os.ReadFile(args[1])
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		img, e := render.ParsePGM(data, 1664, 2233)
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		stream, e := protocol.EncodeNativeJob(img.Gray, img.Width, img.Height, protocol.Settings{Speed: 12, Density: 9, Copies: 1})
		if e == nil {
			decoded, decodeErr := protocol.DecodeJob(stream)
			e = decodeErr
			if e == nil && (decoded.WidthBytes != 208 || decoded.Height != img.Height) {
				e = fmt.Errorf("decoded geometry mismatch")
			}
		}
		if e == nil {
			e = os.WriteFile(args[2], stream, 0600)
		}
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		emit(result{OK: true, Command: "encode-pgm", Detail: map[string]any{"output": filepath.Base(args[2]), "bytes": len(stream)}}, 0)
	case "validate":
		if len(args) != 2 {
			emit(result{Error: "validate stream.f11"}, 2)
		}
		info, e := os.Stat(args[1])
		if e != nil || info.Size() <= 0 || info.Size() > usb.MaxStreamBytes {
			emit(result{Error: "invalid stream file size"}, 1)
		}
		stream, e := os.ReadFile(args[1])
		var decoded protocol.DecodedJob
		if e == nil {
			decoded, e = protocol.DecodeJob(stream)
		}
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		sum := sha256.Sum256(stream)
		emit(result{OK: true, Command: "validate", Detail: map[string]any{"bytes": len(stream), "rows": decoded.Height, "width_bytes": decoded.WidthBytes, "sha256": hex.EncodeToString(sum[:])}}, 0)
	case "send":
		if len(args) != 2 {
			emit(result{Error: "send validated.f11"}, 2)
		}
		info, e := os.Stat(args[1])
		if e != nil || info.Size() <= 0 || info.Size() > usb.MaxStreamBytes {
			emit(result{Error: "invalid stream file size"}, 1)
		}
		stream, e := os.ReadFile(args[1])
		if e == nil {
			_, e = protocol.DecodeJob(stream)
		}
		if e != nil {
			emit(result{Error: "stream validation: " + e.Error()}, 1)
		}
		if e = usb.Send(usb.NewUSBFS(), stream, 2048); e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		emit(result{OK: true, Command: "send", Detail: map[string]any{"bytes": len(stream)}}, 0)
	case "diagnose":
		r := selfTest()
		checks := map[string]any{"protocol": r.Detail}
		b := usb.NewUSBFS()
		d, e := usb.Probe(b)
		usbOK := e == nil
		if !usbOK {
			checks["usb"] = map[string]any{"ok": false, "error": e.Error()}
		} else {
			checks["usb"] = map[string]any{"ok": true, "device": d}
		}
		overall := r.OK && usbOK
		emit(result{OK: overall, Command: "diagnose", Detail: checks}, map[bool]int{true: 0, false: 1}[overall])
	default:
		emit(result{Error: "unknown command"}, 2)
	}
}
