import Foundation
import F11PrintCore

@main struct CoreTests {
    static func main() throws {
        var failures = 0
        func check(_ condition: @autoclosure () throws -> Bool, _ name: String) {
            do {
                if try condition() { print("PASS \(name)") }
                else { failures += 1; fputs("FAIL \(name)\n", stderr) }
            } catch {
                failures += 1
                fputs("FAIL \(name): \(error)\n", stderr)
            }
        }

        check(F11CRC.checksum(Data([0x11, 0x05, 0x11, 0x01, 0x00, 0x00])) == 0x271c4131, "seeded CRC golden vector")
        let frame = F11Frame(appClass: 0x11, command: 5, subcommand: 0x11, payload: Data([0]))
        check(frame.encoded.hex == "a31e1c00060011051101000031411c27", "outer frame golden bytes")

        let symbols = Data([0, 0, 0, 255, 255, 17, 17, 17, 17])
        let tree = F11Huffman.build(from: symbols)
        let packed = tree.encode(symbols)
        check(tree.decode(packed.bytes, paddingBits: packed.paddingBits, count: symbols.count) == symbols, "Huffman round trip")
        check(tree.serializedTraversals.count == tree.nodeCount * 4, "tree traversal serialization")

        let row = Data((0..<199).map { UInt8(truncatingIfNeeded: $0 * 37) })
        let rowTree = F11Huffman.build(from: row)
        let rowPacket = F11JobEncoder.rowFrame(index: 2, row: row, tree: rowTree)
        check(F11JobDecoder.decodeRow(rowPacket, tree: rowTree) == row, "199-byte row round trip")

        let white = [UInt8](repeating: 255, count: 1664 * 24)
        let whiteJob = try F11JobEncoder.encode(gray: white, sourceWidth: 1664, sourceHeight: 24, speed: 16, density: 8, tracking: 0)
        let whiteDecoded = try F11JobDecoder.decode(whiteJob)
        check(whiteDecoded.widthBytes == 199 && whiteDecoded.height == 24, "clean-room geometry decodes")
        check(whiteDecoded.rows.count == 24 && whiteDecoded.rows.allSatisfy { $0.count == 199 }, "all clean-room rows decode")

        var gray = [UInt8](repeating: 255, count: 1664 * 32)
        for y in 4..<28 {
            for x in 200..<1464 where (x + y) % 7 < 3 { gray[y * 1664 + x] = UInt8((x + y) & 255) }
        }
        let expected = try F11JobEncoder.monochrome(gray: gray, width: 1664, height: 32)
        let stream = try F11JobEncoder.encode(gray: gray, sourceWidth: 1664, sourceHeight: 32, speed: 16, density: 8, tracking: 0)
        check(try F11JobDecoder.decode(stream).rows == expected, "generated rows equal intended raster")

        check((try? F11JobEncoder.monochrome(gray: [255], width: 1664, height: 1)) == nil, "invalid dimensions rejected")
        check((try? F11JobEncoder.encode(gray: white, sourceWidth: 1664, sourceHeight: 24, speed: 256, density: 8, tracking: 0)) == nil, "invalid speed rejected")
        check((try? F11JobEncoder.encode(gray: white, sourceWidth: 1664, sourceHeight: 24, speed: 16, density: 0, tracking: 0)) == nil, "invalid density rejected")
        check((try? F11JobEncoder.encode(gray: white, sourceWidth: 1664, sourceHeight: 24, speed: 16, density: 8, tracking: 0, copies: 256)) == nil, "invalid copies rejected")
        check(tree.decode(Data(), paddingBits: 8, count: 1).isEmpty, "invalid Huffman padding rejected")
        check(tree.decode(Data([0]), paddingBits: 0, count: 9).isEmpty, "truncated Huffman stream rejected")
        var corrupt = whiteJob
        corrupt[corrupt.startIndex + 12] ^= 1
        check((try? F11JobDecoder.decode(corrupt)) == nil, "corrupt frame rejected")

        var shortBody=Data([0xa3,0x1e,0x1c,0,4,0,0,0,0,0])
        let shortCRC=F11CRC.checksum(Data([0,0,0,0]));shortBody.appendLE(shortCRC)
        check((try? F11JobDecoder.decode(shortBody)) == nil, "CRC-valid short body rejected")
        var wrongType=whiteJob;wrongType[wrongType.startIndex+3]=1
        check((try? F11JobDecoder.decode(wrongType)) == nil, "nonzero packet type rejected")
        check(F11Frame(appClass:0x11,command:5,subcommand:1,payload:Data(repeating:0,count:65_531)).encoded.isEmpty, "oversized frame payload rejected")

        let rect = PageRenderer.fitRect(source: CGSize(width: 612, height: 792), canvas: CGSize(width: 1664, height: 2233), margin: 72)
        check(abs(rect.midX - 832) < 0.01 && abs(rect.midY - 1116.5) < 0.01, "PDF fit centers")
        check(rect.width <= 1520.01 && rect.height <= 2089.01, "PDF fit honors margins")
        check(PageRenderer.orientationDecision(source: CGSize(width: 792, height: 612), canvas: CGSize(width: 1664, height: 2233), margin: 72).rotate, "landscape rotation")

        var pixels = [UInt8](repeating: 255, count: 16); pixels[5] = 0
        let shifted = PageRenderer.translateGray(pixels, width: 8, height: 2, dx: -2, dy: 0)
        check(shifted[3] == 0 && Array(shifted[6..<8]) == [255, 255], "calibration translation")
        check((try? PrintRequest(pdf: URL(fileURLWithPath: "/tmp/file.txt"))) == nil, "non-PDF rejected")

        if failures > 0 {
            fputs("\(failures) TEST(S) FAILED\n", stderr)
            exit(1)
        }
        print("ALL CORE TESTS PASS")
    }
}
