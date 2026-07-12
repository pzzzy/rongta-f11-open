package protocol

import (
	"bytes"
	"encoding/hex"
	"os"
	"testing"
)

func TestSeededCRCGolden(t *testing.T) {
	body := []byte{0x11, 0x05, 0x11, 0x01, 0x00, 0x00}
	if got := Checksum(body); got != 0x271c4131 {
		t.Fatalf("CRC=%08x", got)
	}
}

func TestFrameGolden(t *testing.T) {
	got, err := EncodeFrame(Frame{AppClass: 0x11, Command: 5, Subcommand: 0x11, Payload: []byte{0}})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString("a31e1c00060011051101000031411c27")
	if !bytes.Equal(got, want) {
		t.Fatalf("frame=%x", got)
	}
}

func TestHuffmanRoundTripAndTraversal(t *testing.T) {
	input := []byte{0, 0, 0, 255, 255, 17, 17, 17, 17}
	tree := BuildHuffman(input)
	packed, pad := tree.Encode(input)
	got, err := tree.Decode(packed, pad, len(input))
	if err != nil || !bytes.Equal(got, input) {
		t.Fatalf("decode=%x err=%v", got, err)
	}
	if len(tree.SerializedTraversals()) != tree.NodeCount()*4 {
		t.Fatal("traversal size")
	}
}

func TestRowRoundTrip(t *testing.T) {
	row := make([]byte, 199)
	for i := range row {
		row[i] = byte(i * 37)
	}
	tree := BuildHuffman(row)
	packet, err := EncodeRowFrame(2, row, tree)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRow(packet, tree)
	if err != nil || !bytes.Equal(got, row) {
		t.Fatalf("row mismatch %v", err)
	}
}

func TestJobRoundTripMatchesIntendedRows(t *testing.T) {
	const width, height = 1664, 32
	gray := bytes.Repeat([]byte{255}, width*height)
	for y := 4; y < 28; y++ {
		for x := 200; x < 1464; x++ {
			if (x+y)%7 < 3 {
				gray[y*width+x] = byte(x + y)
			}
		}
	}
	intended, err := Monochrome(gray, width, height)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := EncodeJob(gray, width, height, Settings{Speed: 16, Density: 8, Copies: 1})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeJob(stream)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.WidthBytes != 199 || decoded.Height != height || len(decoded.Rows) != height {
		t.Fatal("geometry")
	}
	for i := range intended {
		if !bytes.Equal(decoded.Rows[i], intended[i]) {
			t.Fatalf("row %d", i)
		}
	}
}

func TestRejectsCorruptAndInvalidInputs(t *testing.T) {
	if _, err := Monochrome([]byte{255}, 1664, 1); err == nil {
		t.Fatal("dimensions accepted")
	}
	gray := bytes.Repeat([]byte{255}, 1664*2)
	if _, err := EncodeJob(gray, 1664, 2, Settings{Speed: 256, Density: 8, Copies: 1}); err == nil {
		t.Fatal("speed accepted")
	}
	if _, err := EncodeJob(gray, 1664, 2, Settings{Speed: 16, Density: 0, Copies: 1}); err == nil {
		t.Fatal("density accepted")
	}
	if _, err := EncodeJob(gray, 1664, 2, Settings{Speed: 16, Density: 8, Copies: 256}); err == nil {
		t.Fatal("copies accepted")
	}
	stream, _ := EncodeJob(gray, 1664, 2, Settings{Speed: 16, Density: 8, Copies: 1})
	stream[12] ^= 1
	if _, err := DecodeJob(stream); err == nil {
		t.Fatal("CRC corruption accepted")
	}
}

func TestFullNativeWidthJobRoundTrip(t *testing.T) {
	gray := bytes.Repeat([]byte{255}, 1664*15)
	stream, err := EncodeNativeJob(gray, 1664, 15, Settings{Speed: 12, Density: 9, Copies: 1})
	if err != nil {
		t.Fatal(err)
	}
	job, err := DecodeJob(stream)
	if err != nil {
		t.Fatal(err)
	}
	if job.WidthBytes != 208 || job.Height != 15 || len(job.Rows) != 15 {
		t.Fatalf("%+v", job)
	}
}

func TestStrictValidationRejectsUnknownAndMalformedFrames(t *testing.T) {
	gray := bytes.Repeat([]byte{255}, 1664*2)
	stream, err := EncodeJob(gray, 1664, 2, Settings{Speed: 16, Density: 8, Copies: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeJob(stream); err != nil {
		t.Fatal(err)
	}
	extra, _ := EncodeFrame(Frame{AppClass: 0x11, Command: 0x7f, Subcommand: 0x7f, Payload: []byte{1}})
	if _, err := DecodeJob(append(append([]byte(nil), stream...), extra...)); err == nil {
		t.Fatal("extra command accepted")
	}
	frames, err := decodeFrames(stream)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func([]Frame)
	}{
		{"wrong setup app", func(f []Frame) { f[3].AppClass = 0x10 }},
		{"wrong setup command", func(f []Frame) { f[3].Command = 4 }},
		{"bad sentinel", func(f []Frame) { f[5].Payload[5] = 198 }},
		{"bad row index", func(f []Frame) { f[6].Payload[0] = 2 }},
		{"bad row reserved", func(f []Frame) { f[6].Payload[6] = 1 }},
		{"bad end payload", func(f []Frame) { f[len(f)-2].Payload = []byte{1} }},
	}
	for _, m := range mutations {
		copyFrames := make([]Frame, len(frames))
		for i, f := range frames {
			copyFrames[i] = Frame{f.AppClass, f.Command, f.Subcommand, append([]byte(nil), f.Payload...)}
		}
		m.edit(copyFrames)
		var wire []byte
		for _, f := range copyFrames {
			x, _ := EncodeFrame(f)
			wire = append(wire, x...)
		}
		if _, err := DecodeJob(wire); err == nil {
			t.Fatalf("%s accepted", m.name)
		}
	}
}

func TestRejectsOversizedTree(t *testing.T) {
	payload := make([]byte, 512*4)
	f, _ := EncodeFrame(Frame{AppClass: 0x11, Command: 5, Subcommand: 0x0e, Payload: payload})
	if _, err := treeFrom(payload); err == nil {
		t.Fatal("oversized tree accepted")
	}
	_ = f
}

func TestDeterministicSelfTestJobGolden(t *testing.T) {
	const width, height = 1592, 8
	gray := bytes.Repeat([]byte{255}, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if (x+y)%17 == 0 {
				gray[y*width+x] = 0
			}
		}
	}
	stream, err := EncodeJob(gray, width, height, Settings{Speed: 16, Density: 8, Copies: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(stream) != 913 {
		t.Fatalf("bytes=%d", len(stream))
	}
	fixture, err := os.ReadFile("../../testdata/swift-selftest.f11")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stream, fixture) {
		t.Fatalf("Go output differs from Swift fixture")
	}
}
