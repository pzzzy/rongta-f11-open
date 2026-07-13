package banner

import (
	_ "embed"
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

type FontStyle string

const (
	FontGoBold    FontStyle = "bold"
	FontComicSans FontStyle = "comic-sans"
)

type Layout struct {
	Lines                               []string
	LogicalWidth, LogicalHeight, Margin int
	FontSize                            float64
	Font                                FontStyle
}

// Comic Neue is an OFL-licensed Comic Sans-style face, not Microsoft's font.
//
//go:embed assets/ComicNeue-Bold.otf
var comicNeueBold []byte

var parsedGoBold = mustParse(gobold.TTF)
var parsedComicNeue = mustParse(comicNeueBold)

func mustParse(data []byte) *opentype.Font {
	f, err := opentype.Parse(data)
	if err != nil {
		panic(err)
	}
	return f
}

func face(style FontStyle, size float64) (font.Face, error) {
	var parsed *opentype.Font
	switch style {
	case FontGoBold:
		parsed = parsedGoBold
	case FontComicSans:
		parsed = parsedComicNeue
	default:
		return nil, errors.New("unknown banner font")
	}
	return opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 203, Hinting: font.HintingFull})
}

func SupportsText(style FontStyle, text string) bool {
	f, err := face(style, 12)
	if err != nil {
		return false
	}
	defer f.Close()
	for _, r := range text {
		if r == ' ' {
			continue
		}
		if _, ok := f.GlyphAdvance(r); !ok {
			return false
		}
	}
	return true
}

func measure(text string, style FontStyle, size float64) (int, int, error) {
	f, err := face(style, size)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	d := font.Drawer{Face: f}
	b, _ := d.BoundString(text)
	return (b.Max.X - b.Min.X).Ceil(), (f.Metrics().Ascent + f.Metrics().Descent).Ceil(), nil
}

func bestSize(lines []string, style FontStyle, w, h, margin int) float64 {
	lo, hi := 8.0, 1000.0
	gap := 45
	for i := 0; i < 28; i++ {
		mid := (lo + hi) / 2
		maxW, lineH := 0, 0
		for _, line := range lines {
			mw, mh, _ := measure(line, style, mid)
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

// Plan preserves the legacy two-line banner heuristic used by f11d banner.
func Plan(text string, w, h, margin int) (Layout, error) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || len(text) > 256 || w <= 0 || h <= 0 || margin < 0 {
		return Layout{}, errors.New("invalid banner")
	}
	words := strings.Fields(text)
	if len(words) < 2 {
		return Layout{words, w, h, margin, bestSize(words, FontGoBold, w, h, margin), FontGoBold}, nil
	}
	if len(words) > 2 && strings.EqualFold(words[0], "PLEASE") && strings.EqualFold(words[1], "DON'T") {
		lines := []string{strings.Join(words[:2], " "), strings.Join(words[2:], " ")}
		return Layout{lines, w, h, margin, bestSize(lines, FontGoBold, w, h, margin), FontGoBold}, nil
	}
	var best Layout
	bestScore := -1.0
	for cut := 1; cut < len(words); cut++ {
		lines := []string{strings.Join(words[:cut], " "), strings.Join(words[cut:], " ")}
		size := bestSize(lines, FontGoBold, w, h, margin)
		score := size
		mw1, _, _ := measure(lines[0], FontGoBold, size)
		mw2, _, _ := measure(lines[1], FontGoBold, size)
		balance := 1 - math.Abs(float64(mw1-mw2))/float64(max(mw1, mw2))
		score *= 0.8 + 0.2*balance
		if strings.HasPrefix(lines[0], "PLEASE ") && len(strings.Fields(lines[0])) == 2 {
			score *= 1.35
		}
		if score > bestScore {
			bestScore = score
			best = Layout{lines, w, h, margin, size, FontGoBold}
		}
	}
	return best, nil
}

func linePartitions(words []string, count int) [][]string {
	if count == 1 {
		return [][]string{{strings.Join(words, " ")}}
	}
	var out [][]string
	for cut := 1; cut <= len(words)-count+1; cut++ {
		first := strings.Join(words[:cut], " ")
		for _, rest := range linePartitions(words[cut:], count-1) {
			out = append(out, append([]string{first}, rest...))
		}
	}
	return out
}

// PlanLines chooses the largest word-preserving layout. lineCount 0 means auto (1-3).
func PlanLines(text string, w, h, margin, lineCount int, style FontStyle) (Layout, error) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || len(text) > 256 || w <= 0 || h <= 0 || margin < 0 || (style != FontGoBold && style != FontComicSans) {
		return Layout{}, errors.New("invalid banner")
	}
	words := strings.Fields(text)
	if len(words) > 16 {
		return Layout{}, errors.New("too many words")
	}
	if lineCount < 0 || lineCount > 3 || (lineCount > 0 && lineCount > len(words)) {
		return Layout{}, errors.New("invalid line count")
	}
	first, last := lineCount, lineCount
	if lineCount == 0 {
		first, last = 1, min(3, len(words))
	}
	var best Layout
	bestSizeSeen, bestBalance := -1.0, -1.0
	for count := first; count <= last; count++ {
		for _, lines := range linePartitions(words, count) {
			size := bestSize(lines, style, w, h, margin)
			minW, maxW := -1, 0
			for _, line := range lines {
				mw, _, _ := measure(line, style, size)
				if minW < 0 || mw < minW {
					minW = mw
				}
				if mw > maxW {
					maxW = mw
				}
			}
			balance := float64(minW) / float64(maxW)
			if size > bestSizeSeen+0.001 || (math.Abs(size-bestSizeSeen) <= 0.001 && balance > bestBalance) {
				bestSizeSeen, bestBalance = size, balance
				best = Layout{lines, w, h, margin, size, style}
			}
		}
	}
	if len(best.Lines) == 0 {
		return Layout{}, errors.New("banner does not fit")
	}
	return best, nil
}

func Render(l Layout) ([]byte, error) {
	img := image.NewGray(image.Rect(0, 0, l.LogicalWidth, l.LogicalHeight))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	style := l.Font
	if style == "" {
		style = FontGoBold
	}
	f, err := face(style, l.FontSize)
	if err != nil {
		return nil, err
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
