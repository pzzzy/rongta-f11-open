package giftpage

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"sort"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	Width        = 1664
	Height       = 2233
	MaxDisplayed = 24
)

type Celebration struct {
	Total      int
	Gifter     string
	Recipients []string
	Missing    int
}

type Report struct {
	Gifter    string
	Total     int
	Displayed int
	Missing   int
	More      int
}

var boldFont = mustParse(gobold.TTF)
var regularFont = mustParse(goregular.TTF)

func mustParse(data []byte) *opentype.Font {
	f, err := opentype.Parse(data)
	if err != nil {
		panic(err)
	}
	return f
}

func makeFace(bold bool, size float64) (font.Face, error) {
	f := regularFont
	if bold {
		f = boldFont
	}
	return opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
}

func clean(s string) string {
	var out strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r >= 32 && r <= 126 {
			out.WriteRune(r)
		} else {
			out.WriteByte(' ')
		}
		if out.Len() >= 25 {
			break
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

func cleanNames(in []string) []string {
	var out []string
	for _, raw := range in {
		n := clean(raw)
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

func fill(img *image.Gray, r image.Rectangle, shade uint8) {
	r = r.Intersect(img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetGray(x, y, color.Gray{Y: shade})
		}
	}
}

func hline(img *image.Gray, x1, x2, y, w int) { fill(img, image.Rect(x1, y, x2, y+w), 0) }
func vline(img *image.Gray, x, y1, y2, w int) { fill(img, image.Rect(x, y1, x+w, y2), 0) }
func box(img *image.Gray, r image.Rectangle, w int) {
	hline(img, r.Min.X, r.Max.X, r.Min.Y, w)
	hline(img, r.Min.X, r.Max.X, r.Max.Y-w, w)
	vline(img, r.Min.X, r.Min.Y, r.Max.Y, w)
	vline(img, r.Max.X-w, r.Min.Y, r.Max.Y, w)
}

func measure(face font.Face, s string) int { return font.MeasureString(face, s).Ceil() }

func fitFace(text string, bold bool, maxSize, minSize float64, maxWidth int) (font.Face, float64, error) {
	for size := maxSize; size >= minSize; size -= 2 {
		f, err := makeFace(bold, size)
		if err != nil {
			return nil, 0, err
		}
		if measure(f, text) <= maxWidth {
			return f, size, nil
		}
		f.Close()
	}
	f, err := makeFace(bold, minSize)
	if err != nil {
		return nil, 0, err
	}
	if measure(f, text) > maxWidth {
		f.Close()
		return nil, 0, errors.New("text does not fit bounded region")
	}
	return f, minSize, nil
}

func drawCentered(img *image.Gray, text string, bold bool, size float64, y int) error {
	f, err := makeFace(bold, size)
	if err != nil {
		return err
	}
	defer f.Close()
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.Gray{Y: 0}), Face: f}
	w := d.MeasureString(text).Ceil()
	d.Dot = fixed.P((Width-w)/2, y+f.Metrics().Ascent.Ceil())
	d.DrawString(text)
	return nil
}

func drawCenteredFit(img *image.Gray, text string, bold bool, maxSize, minSize float64, y, maxWidth int) (float64, error) {
	f, size, err := fitFace(text, bold, maxSize, minSize, maxWidth)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.Gray{Y: 0}), Face: f}
	w := d.MeasureString(text).Ceil()
	d.Dot = fixed.P((Width-w)/2, y+f.Metrics().Ascent.Ceil())
	d.DrawString(text)
	return size, nil
}

func drawAt(img *image.Gray, text string, bold bool, size float64, x, y int) error {
	f, err := makeFace(bold, size)
	if err != nil {
		return err
	}
	defer f.Close()
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.Gray{Y: 0}), Face: f}
	d.Dot = fixed.P(x, y+f.Metrics().Ascent.Ceil())
	d.DrawString(text)
	return nil
}

func confetti(img *image.Gray) {
	// One consistent, sparse starburst vocabulary: pluses and stepped diamonds.
	for _, p := range [][2]int{{160, 205}, {1504, 205}, {145, 610}, {1519, 610}} {
		x, y := p[0], p[1]
		hline(img, x-18, x+18, y-3, 6)
		vline(img, x-3, y-18, y+18, 6)
	}
	for _, p := range [][2]int{{225, 370}, {1439, 370}} {
		x, y := p[0], p[1]
		hline(img, x-14, x+14, y-3, 6)
		hline(img, x-9, x+9, y-11, 6)
		hline(img, x-9, x+9, y+5, 6)
	}
}

func Render(c Celebration) (*image.Gray, Report, error) {
	c.Gifter = clean(c.Gifter)
	names := cleanNames(c.Recipients)
	if c.Total < 10 || c.Total > 1000 || c.Gifter == "" || len(names) == 0 || c.Missing < 0 {
		return nil, Report{}, errors.New("invalid gift celebration")
	}
	img := image.NewGray(image.Rect(0, 0, Width, Height))
	fill(img, img.Rect, 255)
	box(img, image.Rect(72, 72, Width-72, Height-72), 8)
	box(img, image.Rect(94, 94, Width-94, Height-94), 3)
	confetti(img)
	if err := drawCentered(img, "A COMMUNITY MOMENT", false, 36, 112); err != nil {
		return nil, Report{}, err
	}
	hline(img, 360, Width-360, 185, 5)
	if _, err := drawCenteredFit(img, fmt.Sprintf("%d", c.Total), true, 300, 180, 230, 1150); err != nil {
		return nil, Report{}, err
	}
	if err := drawCentered(img, "GIFT SUBS", true, 78, 545); err != nil {
		return nil, Report{}, err
	}
	if err := drawCentered(img, "GIFTED BY", false, 34, 705); err != nil {
		return nil, Report{}, err
	}
	nameBox := image.Rect(180, 770, Width-180, 1010)
	box(img, nameBox, 6)
	if _, err := drawCenteredFit(img, strings.ToUpper(c.Gifter), true, 112, 30, 815, nameBox.Dx()-90); err != nil {
		return nil, Report{}, err
	}
	hline(img, 165, Width-165, 1080, 5)
	if err := drawCentered(img, "WELCOME TO THE CREW", true, 54, 1125); err != nil {
		return nil, Report{}, err
	}

	display := names
	if len(display) > MaxDisplayed {
		display = display[:MaxDisplayed]
	}
	more := c.Missing + max(0, len(names)-len(display)) + max(0, c.Total-len(names)-c.Missing)
	// Compact roster typography scales down only when necessary.
	fontSize := 40.0
	if len(display) > 16 {
		fontSize = 34
	}
	if len(display) > 20 {
		fontSize = 30
	}
	rows := (len(display) + 1) / 2
	startY := 1245
	rowGap := 76
	if rows > 8 {
		rowGap = 64
	}
	if rows > 10 {
		rowGap = 54
	}
	colX := []int{210, 865}
	for i, n := range display {
		col := i % 2
		row := i / 2
		prefix := fmt.Sprintf("%02d", i+1)
		if err := drawAt(img, prefix, false, fontSize*0.72, colX[col], startY+row*rowGap); err != nil {
			return nil, Report{}, err
		}
		f, _, err := fitFace(n, true, fontSize, 14, 510)
		if err != nil {
			return nil, Report{}, err
		}
		d := font.Drawer{Dst: img, Src: image.NewUniform(color.Gray{Y: 0}), Face: f}
		d.Dot = fixed.P(colX[col]+70, startY+row*rowGap+f.Metrics().Ascent.Ceil())
		d.DrawString(n)
		f.Close()
	}
	rosterBottom := startY + rows*rowGap
	if more > 0 {
		if err := drawCentered(img, fmt.Sprintf("+ %d MORE", more), true, 36, rosterBottom+20); err != nil {
			return nil, Report{}, err
		}
		rosterBottom += 70
	}
	footerY := max(rosterBottom+70, 2020)
	hline(img, 320, Width-320, footerY-35, 4)
	if err := drawCentered(img, "THANK YOU FOR GROWING THE COMMUNITY", false, 30, footerY); err != nil {
		return nil, Report{}, err
	}
	return img, Report{Gifter: c.Gifter, Total: c.Total, Displayed: len(display), Missing: c.Missing, More: more}, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func SortedRecipients(names []string) []string {
	out := cleanNames(names)
	sort.Strings(out)
	return out
}
