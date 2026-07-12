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
func TestRejectMalformedOrOversizedPGM(t *testing.T) {
	cases := [][]byte{[]byte("P2\n1 1\n255\n0"), []byte("P5\n1664 2\n256\n"), []byte("P5\n1664 2\n255\nshort"), []byte("P5\n1665 2\n255\n" + string(bytes.Repeat([]byte{0}, 3330)))}
	for i, c := range cases {
		if _, err := ParsePGM(c, 1664, 2); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
