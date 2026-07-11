# F11 wire protocol

This document describes the clean-room interoperability subset used by the tested Rongta F11. Multibyte integers are little-endian unless stated otherwise.

## USB transport

```text
VID:       0x0FE6
PID:       0x811E
Interface: 0
Class:     USB Printer Class 07/01/02
Bulk OUT:  0x01
Bulk IN:   0x81
```

Transport chunk boundaries are not protocol records. The reference sender uses checked 2,048-byte bulk writes with 4 ms pacing.

## Outer frame

```text
offset  size  field
0       2     sync: A3 1E
2       1     printer type: 1C
3       1     packet type: 00
4       2     body length
6       1     application class
7       1     command
8       1     subcommand
9       2     payload length
11      N     payload
11+N    4     CRC-32
```

`body_length = payload_length + 5` and total frame size is `payload_length + 15`.

The CRC is compatible with:

```python
zlib.crc32(body, 0x76953521) & 0xffffffff
```

It covers application class through the end of payload and is stored little-endian.

## One-page sequence

| Class | Command | Subcommand | Purpose |
|---:|---:|---:|---|
| `11` | `05` | `11` | media tracking |
| `11` | `05` | `07` | speed |
| `11` | `05` | `04` | density |
| `11` | `05` | `0B` | image geometry/start |
| `11` | `05` | `0E` | Huffman tree |
| `10` | `05` | `0D` | sentinel and image rows |
| `11` | `05` | `0C` | image end |
| `11` | `05` | `08` | print/commit |

### Settings

```text
05/11: tracking
05/07: speed
05/04: 03 density 08
```

### Image start

```text
uint16 width_dots
uint16 height_rows
uint8  compression   # observed 0
uint8  direction     # observed 0
uint8  flag          # observed 1
```

The tested Letter path uses width 1592 dots (`0x0638`), or 199 packed bytes per row.

### Huffman tree

The `05/0E` payload concatenates two arrays of `uint16` symbols:

```text
preorder[node_count]
inorder[node_count]
```

Leaves represent byte values 0–255. Internal nodes use values above 255. The receiver reconstructs the binary tree from the two traversals.

Rows are encoded independently against a tree built over the complete page's packed row bytes. Codes are consumed most-significant bit first; zero selects the left child and one selects the right child.

### Row record

```text
uint16 row_index
uint16 record_length_after_this_field
uint8  format_version       # 0x11
uint8  width_bytes          # 0xC7 / 199
uint8  reserved[5]          # zero
uint8  unused_low_bits      # 0...7
uint8  Huffman_stream[]
```

A transfer sentinel with row index zero precedes image rows. The clean-room implementation sends one ordinary 199-byte record per source row after the sentinel.

Unused low-order bits in the final compressed byte must be ignored according to `unused_low_bits`.

### End and commit

```text
05/0C payload: empty
05/08 payload: copies 13 00
```

## Geometry and raster convention

The public encoder accepts 8-bit grayscale input and extracts a centered 1592-pixel-wide region from a 1664-pixel canvas. It converts that region to one bit per pixel, most-significant pixel first within each byte. A set bit represents a marked/black pixel.

The reference renderer creates a 1664 × 2233 Letter page at 203 dpi, uses 72-dot content margins, and applies a configurable mechanical calibration shift before protocol encoding.

## Validation

A generated job should be rejected before transport unless:

- every frame has valid sync and lengths;
- every frame CRC matches;
- the Huffman tree reconstructs without duplicate/traversal errors;
- every row decodes to exactly 199 bytes;
- the number of decoded rows equals the declared height; and
- recovered row bytes equal the intended packed raster.

The implementation in `F11Protocol.swift` performs these checks in the reference application.
