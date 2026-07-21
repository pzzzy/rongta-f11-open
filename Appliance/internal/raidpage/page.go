package raidpage

import (
	"errors"
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	Width  = 1664
	Height = 2233
)

type Receipt struct {
	Channel string
	Viewers int
}
type Report struct {
	Channel                  string
	Viewers, WidthDots, Rows int
}

var bold = mustParse(gobold.TTF)

func mustParse(data []byte) *opentype.Font {
	f, err := opentype.Parse(data)
	if err != nil {
		panic(err)
	}
	return f
}
func face(size float64) (font.Face, error) {
	return opentype.NewFace(bold, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
}
func clean(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r < 32 || r > 126 {
			return ""
		}
		b.WriteRune(r)
		if b.Len() >= 25 {
			break
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
func centered(img *image.Gray, text string, size float64, y int) error {
	const innerWidth = Width - 200
	var f font.Face
	var err error
	for current := size; current >= 36; current -= 2 {
		f, err = face(current)
		if err != nil {
			return err
		}
		if font.MeasureString(f, text).Ceil() <= innerWidth {
			break
		}
		if closer, ok := f.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		f = nil
	}
	if f == nil {
		return errors.New("raid receipt text does not fit")
	}
	defer f.Close()
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.Gray{0}), Face: f}
	w := d.MeasureString(text).Ceil()
	d.Dot = fixed.P((Width-w)/2, y+f.Metrics().Ascent.Ceil())
	d.DrawString(text)
	return nil
}
func line(img *image.Gray, x1, y1, x2, y2 int) {
	for x := x1; x <= x2; x++ {
		if x >= 0 && x < Width && y1 >= 0 && y1 < Height {
			img.SetGray(x, y1, color.Gray{0})
		}
	}
}
func Render(r Receipt) ([]byte, Report, error) {
	channel := clean(r.Channel)
	if channel == "" || r.Viewers <= 0 || r.Viewers > 1000000 {
		return nil, Report{}, errors.New("invalid raid receipt")
	}
	img := image.NewGray(image.Rect(0, 0, Width, Height))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	line(img, 100, 105, Width-100, 105)
	line(img, 100, Height-105, Width-100, Height-105)
	if err := centered(img, "RAID INCOMING", 105, 270); err != nil {
		return nil, Report{}, err
	}
	if err := centered(img, strings.ToUpper(channel), 92, 700); err != nil {
		return nil, Report{}, err
	}
	if err := centered(img, "IS RAIDING", 46, 850); err != nil {
		return nil, Report{}, err
	}
	if err := centered(img, formatViewers(r.Viewers), 210, 1080); err != nil {
		return nil, Report{}, err
	}
	if err := centered(img, "VIEWERS", 48, 1370); err != nil {
		return nil, Report{}, err
	}
	if err := centered(img, "WELCOME TO UWOGOOB", 62, 1780); err != nil {
		return nil, Report{}, err
	}
	if err := centered(img, "THANK YOU FOR THE RAID", 38, 1900); err != nil {
		return nil, Report{}, err
	}
	out := make([]byte, Width*Height)
	for py := 0; py < Height; py++ {
		for px := 0; px < Width; px++ {
			out[py*Width+px] = img.GrayAt(px, py).Y
		}
	}
	return out, Report{Channel: strings.ToUpper(channel), Viewers: r.Viewers, WidthDots: Width, Rows: Height}, nil
}
func formatViewers(n int) string {
	s := []byte{}
	for {
		s = append([]byte{byte('0' + n%10)}, s...)
		n /= 10
		if n == 0 {
			break
		}
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = append(s[:i], append([]byte{','}, s[i:]...)...)
	}
	return string(s)
}
