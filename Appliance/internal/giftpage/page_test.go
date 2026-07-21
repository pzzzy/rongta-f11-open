package giftpage

import (
	"image/png"
	"os"
	"testing"
)

func TestRenderLetterCelebrationGeometryCoverageAndContent(t *testing.T) {
	c := Celebration{Total: 10, Gifter: "GENEROUS ONE", Recipients: []string{"Alice", "Bob", "Carol", "Dave", "Eve", "Frank", "Grace", "Heidi", "Ivan", "Judy"}}
	img, report, err := Render(c)
	if err != nil {
		t.Fatal(err)
	}
	if img.Rect.Dx() != Width || img.Rect.Dy() != Height {
		t.Fatalf("geometry=%v", img.Rect)
	}
	if report.Displayed != 10 || report.Missing != 0 || report.Gifter != "GENEROUS ONE" {
		t.Fatalf("report=%#v", report)
	}
	black := 0
	for _, p := range img.Pix {
		if p < 128 {
			black++
		}
	}
	ratio := float64(black) / float64(len(img.Pix))
	if ratio < 0.025 || ratio > 0.24 {
		t.Fatalf("unsafe/uninteresting black coverage %.3f", ratio)
	}
}

func TestRenderCapsRosterAndShowsMore(t *testing.T) {
	var names []string
	for i := 0; i < 40; i++ {
		names = append(names, "Recipient Name")
	}
	_, report, err := Render(Celebration{Total: 50, Gifter: "G", Recipients: names, Missing: 10})
	if err != nil {
		t.Fatal(err)
	}
	if report.Displayed > MaxDisplayed || report.More < 10 {
		t.Fatalf("report=%#v", report)
	}
}

func TestRenderPreservesDistinctRecipientsWithSameDisplayName(t *testing.T) {
	_, report, err := Render(Celebration{Total: 10, Gifter: "G", Recipients: []string{"Same", "Same", "C", "D", "E", "F", "G", "H", "I", "J"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Displayed != 10 || report.More != 0 {
		t.Fatalf("report=%#v", report)
	}
}

func TestMaximumWideNamesAreBoundedAndFit(t *testing.T) {
	wide := "WWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWW"
	names := []string{wide, wide, wide, wide, wide, wide, wide, wide, wide, wide}
	_, report, err := Render(Celebration{Total: 10, Gifter: wide, Recipients: names})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Gifter) != 25 {
		t.Fatalf("gifter was not bounded: %q", report.Gifter)
	}
}

func TestWritePreviewPNG(t *testing.T) {
	img, _, err := Render(Celebration{Total: 10, Gifter: "GIFT HERO", Recipients: []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel", "India", "Juliet"}})
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/preview.png"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(path); err != nil || st.Size() < 1000 {
		t.Fatalf("preview size=%v err=%v", st, err)
	}
}

func TestRejectsInvalidCelebration(t *testing.T) {
	for _, c := range []Celebration{{Total: 9, Gifter: "G"}, {Total: 10, Gifter: ""}, {Total: 10, Gifter: "G", Recipients: nil}} {
		if _, _, err := Render(c); err == nil {
			t.Fatalf("accepted %#v", c)
		}
	}
}
