package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

const crcSeed uint32 = 0x76953521

func Checksum(data []byte) uint32 {
	crc := crcSeed ^ 0xffffffff
	for _, b := range data {
		crc ^= uint32(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xedb88320
			} else {
				crc >>= 1
			}
		}
	}
	return crc ^ 0xffffffff
}

type Frame struct {
	AppClass, Command, Subcommand byte
	Payload                       []byte
}

func EncodeFrame(f Frame) ([]byte, error) {
	if len(f.Payload) > 65530 {
		return nil, errors.New("payload too large")
	}
	body := make([]byte, 5+len(f.Payload))
	body[0] = f.AppClass
	body[1] = f.Command
	body[2] = f.Subcommand
	binary.LittleEndian.PutUint16(body[3:5], uint16(len(f.Payload)))
	copy(body[5:], f.Payload)
	out := make([]byte, 6+len(body)+4)
	copy(out, []byte{0xa3, 0x1e, 0x1c, 0})
	binary.LittleEndian.PutUint16(out[4:6], uint16(len(body)))
	copy(out[6:], body)
	binary.LittleEndian.PutUint32(out[6+len(body):], Checksum(body))
	return out, nil
}

func decodeFrames(data []byte) ([]Frame, error) {
	var out []Frame
	for i := 0; i < len(data); {
		if i+15 > len(data) || data[i] != 0xa3 || data[i+1] != 0x1e || data[i+2] != 0x1c || data[i+3] != 0 {
			return nil, errors.New("sync/type")
		}
		n := int(binary.LittleEndian.Uint16(data[i+4 : i+6]))
		if n < 5 {
			return nil, errors.New("short body")
		}
		end := i + 6 + n + 4
		if end > len(data) {
			return nil, errors.New("truncated")
		}
		body := data[i+6 : i+6+n]
		if Checksum(body) != binary.LittleEndian.Uint32(data[end-4:end]) {
			return nil, errors.New("CRC")
		}
		plen := int(binary.LittleEndian.Uint16(body[3:5]))
		if plen != len(body)-5 {
			return nil, errors.New("length")
		}
		payload := append([]byte(nil), body[5:]...)
		out = append(out, Frame{body[0], body[1], body[2], payload})
		i = end
	}
	return out, nil
}

type node struct {
	symbol        uint16
	weight, order int
	left, right   *node
}

func (n *node) leaf() bool { return n.left == nil && n.right == nil }

type Huffman struct {
	root      *node
	codes     [256][]byte
	nodeCount int
}

func newHuffman(root *node) *Huffman {
	h := &Huffman{root: root}
	var walk func(*node, []byte)
	walk = func(n *node, bits []byte) {
		h.nodeCount++
		if n.leaf() {
			if n.symbol < 256 {
				if len(bits) == 0 {
					h.codes[n.symbol] = []byte{0}
				} else {
					h.codes[n.symbol] = append([]byte(nil), bits...)
				}
			}
			return
		}
		walk(n.left, append(append([]byte(nil), bits...), 0))
		walk(n.right, append(append([]byte(nil), bits...), 1))
	}
	walk(root, nil)
	return h
}
func BuildHuffman(data []byte) *Huffman {
	counts := [256]int{}
	for _, b := range data {
		counts[b]++
	}
	var nodes []*node
	order := 0
	for i, c := range counts {
		if c > 0 {
			nodes = append(nodes, &node{symbol: uint16(i), weight: c, order: order})
			order++
		}
	}
	if len(nodes) == 1 {
		dummy := uint16(0)
		if nodes[0].symbol == 0 {
			dummy = 1
		}
		nodes = append(nodes, &node{symbol: dummy, order: order})
		order++
	}
	if len(nodes) == 0 {
		nodes = []*node{{symbol: 0, weight: 1, order: 0}, {symbol: 1, order: 1}}
		order = 2
	}
	synthetic := uint16(256)
	for len(nodes) > 1 {
		sort.SliceStable(nodes, func(i, j int) bool {
			if nodes[i].weight != nodes[j].weight {
				return nodes[i].weight < nodes[j].weight
			}
			return nodes[i].order < nodes[j].order
		})
		a, b := nodes[0], nodes[1]
		nodes = append(nodes[2:], &node{symbol: synthetic, weight: a.weight + b.weight, order: order, left: a, right: b})
		synthetic++
		order++
	}
	return newHuffman(nodes[0])
}
func (h *Huffman) NodeCount() int { return h.nodeCount }
func (h *Huffman) SerializedTraversals() []byte {
	var pre, mid []uint16
	var p, m func(*node)
	p = func(n *node) {
		pre = append(pre, n.symbol)
		if n.left != nil {
			p(n.left)
		}
		if n.right != nil {
			p(n.right)
		}
	}
	m = func(n *node) {
		if n.left != nil {
			m(n.left)
		}
		mid = append(mid, n.symbol)
		if n.right != nil {
			m(n.right)
		}
	}
	p(h.root)
	m(h.root)
	out := make([]byte, 2*(len(pre)+len(mid)))
	for i, x := range append(pre, mid...) {
		binary.LittleEndian.PutUint16(out[i*2:], x)
	}
	return out
}
func (h *Huffman) Encode(data []byte) ([]byte, byte) {
	var bits []byte
	for _, b := range data {
		bits = append(bits, h.codes[b]...)
	}
	pad := (8 - len(bits)%8) % 8
	for i := 0; i < pad; i++ {
		bits = append(bits, 1)
	}
	out := make([]byte, len(bits)/8)
	for i, b := range bits {
		out[i/8] = (out[i/8] << 1) | b
	}
	return out, byte(pad)
}
func (h *Huffman) Decode(data []byte, pad byte, count int) ([]byte, error) {
	if pad > 7 || count < 0 || len(data)*8 < int(pad) {
		return nil, errors.New("padding")
	}
	valid := len(data)*8 - int(pad)
	cur := h.root
	out := make([]byte, 0, count)
	seen := 0
	for _, b := range data {
		for bit := 7; bit >= 0; bit-- {
			if seen >= valid {
				break
			}
			seen++
			if (b>>bit)&1 == 0 {
				cur = cur.left
			} else {
				cur = cur.right
			}
			if cur == nil {
				return nil, errors.New("tree path")
			}
			if cur.leaf() {
				if cur.symbol >= 256 {
					return nil, errors.New("symbol")
				}
				out = append(out, byte(cur.symbol))
				cur = h.root
			}
		}
	}
	if cur != h.root || len(out) != count {
		return nil, errors.New("decoded length")
	}
	return out, nil
}

func Monochrome(gray []byte, width, height int) ([][]byte, error) {
	if width < 1592 || height <= 0 || height > 65535 || len(gray) != width*height {
		return nil, errors.New("grayscale dimensions")
	}
	bayer := [4][4]int{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}
	x0 := (width - 1592) / 2
	rows := make([][]byte, height)
	for y := 0; y < height; y++ {
		row := make([]byte, 199)
		for x := 0; x < 1592; x++ {
			sx := x + x0
			if sx >= width {
				sx = width - 1
			}
			if int(gray[y*width+sx]) < bayer[y&3][x&3]*16+8 {
				row[x>>3] |= 0x80 >> uint(x&7)
			}
		}
		rows[y] = row
	}
	return rows, nil
}

type Settings struct{ Speed, Density, Tracking, Copies int }
type DecodedJob struct {
	WidthBytes, Height int
	Copies             int
	Rows               [][]byte
}

func frame(c, s byte, p []byte, app byte) ([]byte, error) { return EncodeFrame(Frame{app, c, s, p}) }
func EncodeRowFrame(index int, row []byte, tree *Huffman) ([]byte, error) {
	if (len(row) != 199 && len(row) != 208) || index < 0 || index > 65535 {
		return nil, errors.New("row")
	}
	packed, pad := tree.Encode(row)
	p := make([]byte, 12+len(packed))
	binary.LittleEndian.PutUint16(p, uint16(index))
	binary.LittleEndian.PutUint16(p[2:], uint16(8+len(packed)))
	p[4] = 0x11
	p[5] = byte(len(row))
	p[11] = pad
	copy(p[12:], packed)
	return frame(5, 0x0d, p, 0x10)
}
func EncodeJob(gray []byte, width, height int, s Settings) ([]byte, error) {
	rows, err := Monochrome(gray, width, height)
	if err != nil {
		return nil, err
	}
	return encodeRows(rows, 1592, height, s)
}
func NativeMonochrome(gray []byte, width, height int) ([][]byte, error) {
	if width != 1664 || height <= 0 || height > 65535 || len(gray) != width*height {
		return nil, errors.New("native grayscale dimensions")
	}
	bayer := [4][4]int{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}
	rows := make([][]byte, height)
	for y := 0; y < height; y++ {
		row := make([]byte, 208)
		for x := 0; x < 1664; x++ {
			if int(gray[y*width+x]) < bayer[y&3][x&3]*16+8 {
				row[x>>3] |= 0x80 >> uint(x&7)
			}
		}
		rows[y] = row
	}
	return rows, nil
}
func EncodeNativeJob(gray []byte, width, height int, s Settings) ([]byte, error) {
	rows, err := NativeMonochrome(gray, width, height)
	if err != nil {
		return nil, err
	}
	return encodeRows(rows, 1664, height, s)
}
func encodeRows(rows [][]byte, outputWidth, height int, s Settings) ([]byte, error) {
	if s.Speed < 1 || s.Speed > 255 || s.Density < 1 || s.Density > 15 || s.Tracking < 0 || s.Tracking > 255 || s.Copies < 1 || s.Copies > 255 {
		return nil, errors.New("print settings")
	}
	rowBytes := outputWidth / 8
	all := make([]byte, 0, rowBytes*height)
	for _, r := range rows {
		all = append(all, r...)
	}
	tree := BuildHuffman(all)
	var out []byte
	add := func(c, sub byte, p []byte, app byte) error {
		x, e := frame(c, sub, p, app)
		if e == nil {
			out = append(out, x...)
		}
		return e
	}
	if err := add(5, 0x11, []byte{byte(s.Tracking)}, 0x11); err != nil {
		return nil, err
	}
	_ = add(5, 7, []byte{byte(s.Speed)}, 0x11)
	_ = add(5, 4, []byte{3, byte(s.Density), 8}, 0x11)
	start := make([]byte, 7)
	binary.LittleEndian.PutUint16(start, uint16(outputWidth))
	binary.LittleEndian.PutUint16(start[2:], uint16(height))
	start[6] = 1
	_ = add(5, 0x0b, start, 0x11)
	_ = add(5, 0x0e, tree.SerializedTraversals(), 0x11)
	sent := make([]byte, 12)
	binary.LittleEndian.PutUint16(sent[2:], 8)
	sent[4] = 0x11
	sent[5] = byte(rowBytes)
	sent[11] = 1
	_ = add(5, 0x0d, sent, 0x10)
	for i, r := range rows {
		x, e := EncodeRowFrame(i+1, r, tree)
		if e != nil {
			return nil, e
		}
		out = append(out, x...)
	}
	_ = add(5, 0x0c, nil, 0x11)
	_ = add(5, 8, []byte{byte(s.Copies), 0x13, 0}, 0x11)
	return out, nil
}

func DecodeRow(packet []byte, tree *Huffman) ([]byte, error) {
	fs, err := decodeFrames(packet)
	if err != nil || len(fs) == 0 {
		return nil, fmt.Errorf("frame: %w", err)
	}
	p := fs[0].Payload
	if len(p) < 12 {
		return nil, errors.New("row header")
	}
	return tree.Decode(p[12:], p[11], int(p[5]))
}
func treeFrom(data []byte) (*Huffman, error) {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil, errors.New("tree size")
	}
	n := len(data) / 4
	if n > 511 {
		return nil, errors.New("tree too large")
	}
	pre := make([]uint16, n)
	mid := make([]uint16, n)
	seenPre := map[uint16]bool{}
	seenMid := map[uint16]bool{}
	for i := 0; i < n; i++ {
		pre[i] = binary.LittleEndian.Uint16(data[i*2:])
		mid[i] = binary.LittleEndian.Uint16(data[n*2+i*2:])
		if seenPre[pre[i]] || seenMid[mid[i]] {
			return nil, errors.New("duplicate tree symbols")
		}
		seenPre[pre[i]] = true
		seenMid[mid[i]] = true
	}
	if len(seenPre) != len(seenMid) {
		return nil, errors.New("tree symbols")
	}
	for x := range seenPre {
		if !seenMid[x] {
			return nil, errors.New("tree symbols")
		}
	}
	pi, order := 0, 0
	var build func(int, int) (*node, error)
	build = func(lo, hi int) (*node, error) {
		if lo >= hi || pi >= n {
			return nil, errors.New("tree traversal")
		}
		s := pre[pi]
		pi++
		split := -1
		for i := lo; i < hi; i++ {
			if mid[i] == s {
				split = i
				break
			}
		}
		if split < 0 {
			return nil, errors.New("tree symbol")
		}
		x := &node{symbol: s, order: order}
		order++
		var e error
		if split > lo {
			x.left, e = build(lo, split)
			if e != nil {
				return nil, e
			}
		}
		if split+1 < hi {
			x.right, e = build(split+1, hi)
			if e != nil {
				return nil, e
			}
		}
		return x, nil
	}
	root, err := build(0, n)
	if err != nil || pi != n {
		return nil, errors.New("tree traversal")
	}
	var validate func(*node) error
	validate = func(x *node) error {
		if x.leaf() {
			if x.symbol >= 256 {
				return errors.New("invalid leaf")
			}
			return nil
		}
		if x.left == nil || x.right == nil {
			return errors.New("incomplete tree")
		}
		if e := validate(x.left); e != nil {
			return e
		}
		return validate(x.right)
	}
	if err = validate(root); err != nil {
		return nil, err
	}
	return newHuffman(root), nil
}
func DecodeJob(data []byte) (DecodedJob, error) {
	if len(data) == 0 || len(data) > 64<<20 {
		return DecodedJob{}, errors.New("stream size")
	}
	fs, err := decodeFrames(data)
	if err != nil {
		return DecodedJob{}, err
	}
	if len(fs) < 8 {
		return DecodedJob{}, errors.New("frame count")
	}
	expect := func(i int, app, command, sub byte, size int) ([]byte, error) {
		f := fs[i]
		if f.AppClass != app || f.Command != command || f.Subcommand != sub || (size >= 0 && len(f.Payload) != size) {
			return nil, fmt.Errorf("unexpected frame %d", i)
		}
		return f.Payload, nil
	}
	if p, e := expect(0, 0x11, 5, 0x11, 1); e != nil || p[0] > 255 {
		return DecodedJob{}, errors.New("tracking frame")
	}
	if p, e := expect(1, 0x11, 5, 0x07, 1); e != nil || p[0] == 0 {
		return DecodedJob{}, errors.New("speed frame")
	}
	if p, e := expect(2, 0x11, 5, 0x04, 3); e != nil || p[0] != 3 || p[1] < 1 || p[1] > 15 || p[2] != 8 {
		return DecodedJob{}, errors.New("density frame")
	}
	start, e := expect(3, 0x11, 5, 0x0b, 7)
	if e != nil {
		return DecodedJob{}, e
	}
	w, h := int(binary.LittleEndian.Uint16(start)), int(binary.LittleEndian.Uint16(start[2:]))
	if (w != 1592 && w != 1664) || h <= 0 || start[4] != 0 || start[5] != 0 || start[6] != 1 {
		return DecodedJob{}, errors.New("geometry")
	}
	rowBytes := w / 8
	treePayload, e := expect(4, 0x11, 5, 0x0e, -1)
	if e != nil {
		return DecodedJob{}, e
	}
	tree, e := treeFrom(treePayload)
	if e != nil {
		return DecodedJob{}, e
	}
	if len(fs) != h+8 {
		return DecodedJob{}, errors.New("frame count")
	}
	sentinel, e := expect(5, 0x10, 5, 0x0d, 12)
	if e != nil {
		return DecodedJob{}, e
	}
	wantSentinel := []byte{0, 0, 8, 0, 0x11, byte(rowBytes), 0, 0, 0, 0, 0, 1}
	if !bytes.Equal(sentinel, wantSentinel) {
		return DecodedJob{}, errors.New("sentinel")
	}
	rows := make([][]byte, h)
	for i := 0; i < h; i++ {
		p, e := expect(6+i, 0x10, 5, 0x0d, -1)
		if e != nil {
			return DecodedJob{}, e
		}
		if len(p) < 12 || int(binary.LittleEndian.Uint16(p)) != i+1 || int(binary.LittleEndian.Uint16(p[2:])) != len(p)-4 || p[4] != 0x11 || int(p[5]) != rowBytes || !bytes.Equal(p[6:11], []byte{0, 0, 0, 0, 0}) || p[11] > 7 {
			return DecodedJob{}, errors.New("row header")
		}
		r, e := tree.Decode(p[12:], p[11], rowBytes)
		if e != nil || len(r) != rowBytes {
			return DecodedJob{}, errors.New("row data")
		}
		rows[i] = r
	}
	if _, e = expect(6+h, 0x11, 5, 0x0c, 0); e != nil {
		return DecodedJob{}, e
	}
	copies, e := expect(7+h, 0x11, 5, 0x08, 3)
	if e != nil || copies[0] == 0 || copies[1] != 0x13 || copies[2] != 0 {
		return DecodedJob{}, errors.New("copies frame")
	}
	return DecodedJob{WidthBytes: rowBytes, Height: h, Copies: int(copies[0]), Rows: rows}, nil
}
