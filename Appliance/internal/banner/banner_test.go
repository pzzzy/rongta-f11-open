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
func TestPlanExactLineCounts(t *testing.T) {
	text := "MEETING IN PROGRESS PLEASE WAIT"
	for _, count := range []int{1, 2, 3} {
		layout, err := PlanLines(text, 3045, 1664, 45, count, FontGoBold)
		if err != nil {
			t.Fatalf("lines=%d: %v", count, err)
		}
		if len(layout.Lines) != count {
			t.Fatalf("lines=%d got=%#v", count, layout.Lines)
		}
		joined := ""
		for _, line := range layout.Lines {
			if joined != "" {
				joined += " "
			}
			joined += line
		}
		if joined != text {
			t.Fatalf("lines=%d changed text: %q", count, joined)
		}
	}
}

func TestPlanAutoUsesAtMostThreeLinesAndMaximizesType(t *testing.T) {
	text := "MEETING IN PROGRESS PLEASE WAIT"
	auto, err := PlanLines(text, 3045, 1664, 45, 0, FontGoBold)
	if err != nil {
		t.Fatal(err)
	}
	if len(auto.Lines) < 1 || len(auto.Lines) > 3 {
		t.Fatalf("auto lines=%#v", auto.Lines)
	}
	for count := 1; count <= 3; count++ {
		forced, err := PlanLines(text, 3045, 1664, 45, count, FontGoBold)
		if err != nil {
			t.Fatal(err)
		}
		if auto.FontSize+0.01 < forced.FontSize {
			t.Fatalf("auto %.2f smaller than %d-line %.2f", auto.FontSize, count, forced.FontSize)
		}
	}
}

func TestPlanRejectsImpossibleLinesAndUnknownFont(t *testing.T) {
	if _, err := PlanLines("TWO WORDS", 3045, 1664, 45, 3, FontGoBold); err == nil {
		t.Fatal("accepted more lines than words")
	}
	if _, err := PlanLines("TWO WORDS", 3045, 1664, 45, 2, FontStyle("papyrus")); err == nil {
		t.Fatal("accepted unknown font")
	}
}

func TestComicSansStyleIsExplicitAndNonDefault(t *testing.T) {
	if FontGoBold == FontComicSans || FontGoBold != "bold" || FontComicSans != "comic-sans" {
		t.Fatalf("font constants bold=%q comic=%q", FontGoBold, FontComicSans)
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
