package render

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

type Image struct {
	Width, Height int
	Gray          []byte
}

func FitGrayCanvas(gray []byte, width, height, canvasWidth, canvasHeight int) ([]byte, error) {
	if width <= 0 || height <= 0 || canvasWidth <= 0 || canvasWidth > 1664 || canvasHeight <= 0 || canvasHeight > 2842 || len(gray) != width*height {
		return nil, errors.New("fit dimensions")
	}
	if width == canvasWidth && height == canvasHeight {
		return append([]byte(nil), gray...), nil
	}
	scaleX := float64(canvasWidth) / float64(width)
	scaleY := float64(canvasHeight) / float64(height)
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}
	dstWidth := int(float64(width)*scale + 0.5)
	dstHeight := int(float64(height)*scale + 0.5)
	if dstWidth < 1 {
		dstWidth = 1
	}
	if dstHeight < 1 {
		dstHeight = 1
	}
	out := bytes.Repeat([]byte{255}, canvasWidth*canvasHeight)
	x0 := (canvasWidth - dstWidth) / 2
	y0 := (canvasHeight - dstHeight) / 2
	for y := 0; y < dstHeight; y++ {
		sy := y * height / dstHeight
		for x := 0; x < dstWidth; x++ {
			sx := x * width / dstWidth
			out[(y0+y)*canvasWidth+x0+x] = gray[sy*width+sx]
		}
	}
	return out, nil
}

func FitGrayHeadCanvas(gray []byte, width, height, logicalWidth, logicalHeight, headWidth int) ([]byte, error) {
	if logicalWidth <= 0 || logicalWidth > headWidth || headWidth > 1664 || logicalHeight <= 0 || logicalHeight > 2233 {
		return nil, errors.New("head canvas dimensions")
	}
	logical, err := FitGrayCanvas(gray, width, height, logicalWidth, logicalHeight)
	if err != nil {
		return nil, err
	}
	return CenterPadGray(logical, logicalWidth, logicalHeight, headWidth)
}

func CenterPadGray(gray []byte, width, height, outputWidth int) ([]byte, error) {
	if width <= 0 || height <= 0 || outputWidth < width || len(gray) != width*height {
		return nil, errors.New("padding dimensions")
	}
	out := bytes.Repeat([]byte{255}, outputWidth*height)
	x0 := (outputWidth - width) / 2
	for y := 0; y < height; y++ {
		copy(out[y*outputWidth+x0:], gray[y*width:(y+1)*width])
	}
	return out, nil
}

func token(r *bufio.Reader) (string, error) {
	for {
		b, e := r.ReadByte()
		if e != nil {
			return "", e
		}
		if b == '#' {
			for b != '\n' {
				b, e = r.ReadByte()
				if e != nil {
					return "", e
				}
			}
			continue
		}
		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		var x []byte
		x = append(x, b)
		for {
			b, e = r.ReadByte()
			if e != nil {
				return string(x), nil
			}
			if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
				return string(x), nil
			}
			x = append(x, b)
			if len(x) > 32 {
				return "", errors.New("PGM token too long")
			}
		}
	}
}
func ParsePGM(data []byte, wantWidth, wantHeight int) (Image, error) {
	return ParsePGMHeightRange(data, wantWidth, wantHeight, wantHeight)
}

func ParsePGMHeightRange(data []byte, wantWidth, minHeight, maxHeight int) (Image, error) {
	return ParsePGMRange(data, wantWidth, wantWidth, minHeight, maxHeight)
}

func ParsePGMRange(data []byte, minWidth, maxWidth, minHeight, maxHeight int) (Image, error) {
	r := bufio.NewReader(bytes.NewReader(data))
	magic, e := token(r)
	if e != nil || magic != "P5" {
		return Image{}, errors.New("binary PGM required")
	}
	ws, e := token(r)
	if e != nil {
		return Image{}, e
	}
	hs, e := token(r)
	if e != nil {
		return Image{}, e
	}
	ms, e := token(r)
	if e != nil {
		return Image{}, e
	}
	w, e := strconv.Atoi(ws)
	if e != nil {
		return Image{}, e
	}
	h, e := strconv.Atoi(hs)
	if e != nil {
		return Image{}, e
	}
	max, e := strconv.Atoi(ms)
	if e != nil || max != 255 {
		return Image{}, errors.New("8-bit PGM required")
	}
	if w < minWidth || w > maxWidth || h < minHeight || h > maxHeight || minWidth <= 0 || maxWidth < minWidth || minHeight <= 0 || maxHeight < minHeight {
		return Image{}, fmt.Errorf("unexpected geometry %dx%d", w, h)
	}
	pixels := make([]byte, w*h)
	n, e := io.ReadFull(r, pixels)
	if e != nil || n != len(pixels) {
		return Image{}, errors.New("PGM raster size")
	}
	extra, e := r.ReadByte()
	if e == nil || extra != 0 {
		return Image{}, errors.New("trailing PGM data")
	}
	return Image{w, h, pixels}, nil
}
