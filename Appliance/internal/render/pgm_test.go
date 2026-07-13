package render

import (
	"bytes"
	"testing"
)

func TestParseFullPagePGM(t *testing.T) {
	const w, h = 1664, 2233
	pixels := bytes.Repeat([]byte{0x7f}, w*h)
	input := append([]byte("P5\n1664 2233\n255\n"), pixels...)
	img, err := ParsePGM(input, w, h)
	if err != nil {
		t.Fatal(err)
	}
	if len(img.Gray) != w*h || !bytes.Equal(img.Gray, pixels) {
		t.Fatal("full raster mismatch")
	}
}

func TestParseP5PGM(t *testing.T) {
	pixels := bytes.Repeat([]byte{0x7f}, 1664*2)
	input := append([]byte("P5\n# generated\n1664 2\n255\n"), pixels...)
	image, err := ParsePGM(input, 1664, 2)
	if err != nil {
		t.Fatal(err)
	}
	if image.Width != 1664 || image.Height != 2 || !bytes.Equal(image.Gray, pixels) {
		t.Fatal("image mismatch")
	}
}
func TestFitGrayCanvasPreservesAspectAndCenters(t *testing.T) {
	// 2x4 portrait source into an 8x8 canvas scales uniformly to 4x8.
	src := []byte{0, 10, 20, 30, 40, 50, 60, 70}
	got, err := FitGrayCanvas(src, 2, 4, 8, 8)
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 8; y++ {
		row := got[y*8 : (y+1)*8]
		if row[0] != 255 || row[1] != 255 || row[6] != 255 || row[7] != 255 {
			t.Fatalf("row %d not centered: %v", y, row)
		}
		if row[2] != row[3] || row[4] != row[5] {
			t.Fatalf("row %d nonuniform horizontal scale: %v", y, row)
		}
	}
}

func TestFitGrayCanvasExactIdentity(t *testing.T) {
	src := []byte{0, 1, 2, 3}
	got, err := FitGrayCanvas(src, 2, 2, 2, 2)
	if err != nil || !bytes.Equal(got, src) {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestFitGrayCanvasRejectsInvalidOrHuge(t *testing.T) {
	if _, err := FitGrayCanvas([]byte{1}, 1, 1, 1664, 3000); err == nil {
		t.Fatal("accepted oversized canvas")
	}
	if _, err := FitGrayCanvas([]byte{1}, 2, 1, 10, 10); err == nil {
		t.Fatal("accepted malformed source")
	}
}

func TestParsePGMHeightRange(t *testing.T) {
	pixels := bytes.Repeat([]byte{0x7f}, 1664*96)
	input := append([]byte("P5\n1664 96\n255\n"), pixels...)
	img, err := ParsePGMHeightRange(input, 1664, 1, 2233)
	if err != nil || img.Height != 96 {
		t.Fatalf("img=%+v err=%v", img, err)
	}
	if _, err := ParsePGMHeightRange(input, 1664, 97, 2233); err == nil {
		t.Fatal("height below range accepted")
	}
	if _, err := ParsePGMHeightRange(input, 1664, 1, 95); err == nil {
		t.Fatal("height above range accepted")
	}
}

func TestCenterPadGray(t *testing.T) {
	got, err := CenterPadGray([]byte{1, 2, 3, 4}, 2, 2, 6)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{255, 255, 1, 2, 255, 255, 255, 255, 3, 4, 255, 255}
	if !bytes.Equal(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestCenterPadGrayRejectsWiderInput(t *testing.T) {
	if _, err := CenterPadGray(make([]byte, 7), 7, 1, 6); err == nil {
		t.Fatal("accepted wider input")
	}
}

func TestRejectMalformedOrOversizedPGM(t *testing.T) {
	cases := [][]byte{[]byte("P2\n1 1\n255\n0"), []byte("P5\n1664 2\n256\n"), []byte("P5\n1664 2\n255\nshort"), []byte("P5\n1665 2\n255\n" + string(bytes.Repeat([]byte{0}, 3330)))}
	for i, c := range cases {
		if _, err := ParsePGM(c, 1664, 2); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
