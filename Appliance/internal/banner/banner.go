package banner

import (
	"errors"
	"image"
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type Layout struct {
	Lines                               []string
	LogicalWidth, LogicalHeight, Margin int
	FontSize                            float64
}

var parsedFont = func() *opentype.Font {
	f, e := opentype.Parse(gobold.TTF)
	if e != nil {
		panic(e)
	}
	return f
}()

func face(size float64) (font.Face, error) {
	return opentype.NewFace(parsedFont, &opentype.FaceOptions{Size: size, DPI: 203, Hinting: font.HintingFull})
}
func measure(text string, size float64) (int, int, error) {
	f, e := face(size)
	if e != nil {
		return 0, 0, e
	}
	defer f.Close()
	d := font.Drawer{Face: f}
	b, _ := d.BoundString(text)
	return (b.Max.X - b.Min.X).Ceil(), (f.Metrics().Ascent + f.Metrics().Descent).Ceil(), nil
}
func bestSize(lines []string, w, h, margin int) float64 {
	lo, hi := 8.0, 1000.0
	gap := 45
	for i := 0; i < 28; i++ {
		mid := (lo + hi) / 2
		maxW, lineH := 0, 0
		for _, line := range lines {
			mw, mh, _ := measure(line, mid)
			if mw > maxW {
				maxW = mw
			}
			if mh > lineH {
				lineH = mh
			}
		}
		if maxW <= w-2*margin && lineH*len(lines)+gap*(len(lines)-1) <= h-2*margin {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}
func Plan(text string, w, h, margin int) (Layout, error) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || len(text) > 256 || w <= 0 || h <= 0 || margin < 0 {
		return Layout{}, errors.New("invalid banner")
	}
	words := strings.Fields(text)
	if len(words) < 2 {
		return Layout{words, w, h, margin, bestSize(words, w, h, margin)}, nil
	}
	if len(words) > 2 && strings.EqualFold(words[0], "PLEASE") && strings.EqualFold(words[1], "DON'T") {
		lines := []string{strings.Join(words[:2], " "), strings.Join(words[2:], " ")}
		return Layout{lines, w, h, margin, bestSize(lines, w, h, margin)}, nil
	}
	var best Layout
	bestScore := -1.0
	for cut := 1; cut < len(words); cut++ {
		lines := []string{strings.Join(words[:cut], " "), strings.Join(words[cut:], " ")}
		size := bestSize(lines, w, h, margin)
		score := size
		mw1, _, _ := measure(lines[0], size)
		mw2, _, _ := measure(lines[1], size)
		balance := 1 - math.Abs(float64(mw1-mw2))/float64(max(mw1, mw2))
		score *= 0.8 + 0.2*balance
		if strings.HasPrefix(lines[0], "PLEASE ") && len(strings.Fields(lines[0])) == 2 {
			score *= 1.35
		}
		if score > bestScore {
			bestScore = score
			best = Layout{lines, w, h, margin, size}
		}
	}
	return best, nil
}
func Render(l Layout) ([]byte, error) {
	img := image.NewGray(image.Rect(0, 0, l.LogicalWidth, l.LogicalHeight))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	f, e := face(l.FontSize)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	metrics := f.Metrics()
	lineH := (metrics.Ascent + metrics.Descent).Ceil()
	gap := 45
	total := lineH*len(l.Lines) + gap*(len(l.Lines)-1)
	top := (l.LogicalHeight - total) / 2
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.Gray{0}), Face: f}
	for i, line := range l.Lines {
		width := d.MeasureString(line).Ceil()
		baseline := top + i*(lineH+gap) + metrics.Ascent.Ceil()
		d.Dot = fixed.P((l.LogicalWidth-width)/2, baseline)
		d.DrawString(line)
	}
	out := make([]byte, l.LogicalHeight*l.LogicalWidth)
	printW := l.LogicalHeight
	printH := l.LogicalWidth
	if printW != 1664 {
		return nil, errors.New("logical height must equal printer width")
	}
	for py := 0; py < printH; py++ {
		for px := 0; px < printW; px++ {
			lx, ly := py, l.LogicalHeight-1-px
			out[py*printW+px] = img.GrayAt(lx, ly).Y
		}
	}
	return out, nil
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
