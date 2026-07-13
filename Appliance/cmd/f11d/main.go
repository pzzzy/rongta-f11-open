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
	"strconv"
	"strings"

	"github.com/pzzzy/rongta-f11-open/appliance/internal/banner"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/protocol"
	"github.com/pzzzy/rongta-f11-open/appliance/internal/render"
)

const maxStreamBytes = 64 << 20

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
		emit(result{Error: "usage: f11d self-test|banner|encode-pgm|validate"}, 2)
	}
	switch args[0] {
	case "self-test":
		r := selfTest()
		if !r.OK {
			emit(r, 1)
		}
		emit(r, 0)

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
	case "compose-pgm":
		if len(args) != 4 {
			emit(result{Error: "compose-pgm input.pgm output.pgm canvas-height"}, 2)
		}
		canvasHeight, e := strconv.Atoi(args[3])
		if e != nil || canvasHeight < 20 || canvasHeight > 2842 {
			emit(result{Error: "invalid canvas height"}, 1)
		}
		data, e := os.ReadFile(args[1])
		if e != nil || len(data) > 32<<20 {
			emit(result{Error: "invalid PGM input"}, 1)
		}
		img, e := render.ParsePGMRange(data, 1, 4096, 1, 4096)
		var gray []byte
		if e == nil {
			gray, e = render.FitGrayCanvas(img.Gray, img.Width, img.Height, 1664, canvasHeight)
		}
		if e == nil {
			header := []byte(fmt.Sprintf("P5\n1664 %d\n255\n", canvasHeight))
			e = os.WriteFile(args[2], append(header, gray...), 0600)
		}
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		emit(result{OK: true, Command: "compose-pgm", Detail: map[string]any{"width": 1664, "height": canvasHeight}}, 0)
	case "encode-pgm":
		if len(args) != 3 {
			emit(result{Error: "encode-pgm input.pgm output.f11"}, 2)
		}
		data, e := os.ReadFile(args[1])
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		img, e := render.ParsePGMRange(data, 1, 1664, 1, 2233)
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		gray, e := render.CenterPadGray(img.Gray, img.Width, img.Height, 1664)
		if e != nil {
			emit(result{Error: e.Error()}, 1)
		}
		stream, e := protocol.EncodeNativeJob(gray, 1664, img.Height, protocol.Settings{Speed: 12, Density: 9, Copies: 1})
		if e == nil {
			decoded, decodeErr := protocol.DecodeJob(stream)
			e = decodeErr
			if e == nil && (decoded.WidthBytes != 208 || decoded.Height != img.Height) {
				e = fmt.Errorf("decoded geometry mismatch")
			}
			intended, intendedErr := protocol.NativeMonochrome(gray, 1664, img.Height)
			if e == nil {
				e = intendedErr
			}
			for i := 0; e == nil && i < len(intended); i++ {
				if !bytes.Equal(decoded.Rows[i], intended[i]) {
					e = fmt.Errorf("decoded raster mismatch at row %d", i+1)
				}
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
		if e != nil || info.Size() <= 0 || info.Size() > maxStreamBytes {
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

	default:
		emit(result{Error: "unknown command"}, 2)
	}
}
