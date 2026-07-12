package banner

import "testing"

func TestLayoutTrailerMessage(t *testing.T) {
	layout, err := Plan("PLEASE DON'T PARK YOUR TRAILER HERE", 3045, 1664, 45)
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Lines) != 2 || layout.Lines[0] != "PLEASE DON'T" || layout.Lines[1] != "PARK YOUR TRAILER HERE" {
		t.Fatalf("%#v", layout.Lines)
	}
	if layout.FontSize <= 0 {
		t.Fatal("font size")
	}
}
func TestRejectsEmptyAndOverlong(t *testing.T) {
	if _, err := Plan("", 3045, 1664, 45); err == nil {
		t.Fatal("empty")
	}
	s := ""
	for i := 0; i < 300; i++ {
		s += "X"
	}
	if _, err := Plan(s, 3045, 1664, 45); err == nil {
		t.Fatal("long")
	}
}
func TestRenderLandscapeRotatesToPrinterGeometry(t *testing.T) {
	l, err := Plan("PLEASE DON'T PARK YOUR TRAILER HERE", 3045, 1664, 45)
	if err != nil {
		t.Fatal(err)
	}
	gray, err := Render(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(gray) != 1664*3045 {
		t.Fatalf("pixels=%d", len(gray))
	}
	black := 0
	for _, p := range gray {
		if p < 128 {
			black++
		}
	}
	if black < 10000 {
		t.Fatalf("black=%d", black)
	}
}
