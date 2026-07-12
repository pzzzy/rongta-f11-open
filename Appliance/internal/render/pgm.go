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
	if w != wantWidth || h != wantHeight || w <= 0 || h <= 0 {
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
