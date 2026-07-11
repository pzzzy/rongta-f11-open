# Integration guide

## Library boundaries

`F11PrintCore` contains three layers:

1. `PageRenderer` — optional macOS PDFKit/CoreGraphics rendering.
2. `F11JobEncoder` / `F11JobDecoder` — transport-independent protocol implementation.
3. `PrintEngine` — reference orchestration and fail-closed validation.

`F11PrintCore` never opens a USB device. This keeps protocol generation testable and lets applications choose libusb, a print-server bridge, or another transport.

## Supplying raster data

The encoder accepts row-major 8-bit grayscale:

```swift
let stream = try F11JobEncoder.encode(
    gray: pixels,
    sourceWidth: 1664,
    sourceHeight: pageHeight,
    speed: 16,
    density: 8,
    tracking: 0,
    copies: 1
)
```

The source buffer must contain exactly `sourceWidth * sourceHeight` bytes. The current F11 profile expects a 1664-pixel source width and emits 1592-dot rows.

## Validation before transport

```swift
let expected = try F11JobEncoder.monochrome(
    gray: pixels,
    width: 1664,
    height: pageHeight
)
let decoded = try F11JobDecoder.decode(stream)

guard decoded.rows == expected else {
    throw MyError.generatedStreamMismatch
}
```

Do this even if the stream was generated in the same process. It catches framing, tree, padding, and serialization regressions before hardware sees them.

## USB transport

The included C helper demonstrates conservative transport:

```text
Open VID 0x0FE6 / PID 0x811E
Claim interface 0
Write endpoint 0x01
2,048-byte checked chunks
5-second per-transfer timeout
4 ms between chunks
```

Applications embedding libusb should verify both the return code and `actual_length == requested_length`. Do not treat successful submission as proof of physical output: the tested F11 does not provide a reliable application-level acknowledgement.

## Other platforms

The protocol encoder currently lives in a macOS Swift package because the reference app uses PDFKit and AppKit. The codec itself uses Foundation only and can be split into a platform-neutral target if contributors add Linux support. USB transport is already based on portable libusb C APIs.

A non-Swift implementation should begin with the frame golden vector and decoder-first validation described in `PROTOCOL.md`, then compare generated trees and rows semantically rather than requiring byte identity with this implementation.

## Mechanical calibration

The tested printer needed a 24-dot left correction at the PDF rendering layer. This is not a protocol requirement and may vary across mechanisms. Expose alignment settings in applications instead of hardcoding assumptions into transport code.

## Adding a new model

Do not assume sibling models share F11 geometry or row encoding merely because they use an `A3 1E` prefix. Require:

- official output capture;
- model-routing evidence;
- frame and CRC validation;
- recovered raster dimensions;
- harmless dry-run fixtures; and
- controlled physical verification.
