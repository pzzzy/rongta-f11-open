import Foundation
import CryptoKit
import CoreGraphics
import ImageIO

public struct ToolPaths: Sendable {
    public let usb: URL
    public init(resourceDirectory: URL) {
        usb = resourceDirectory.appendingPathComponent("f11usb")
    }
}

public struct PageResult: Sendable, Codable {
    public let page: Int
    public let streamBytes: Int
    public let sha256: String
    public let printed: Bool
    public let preview: String
}

public final class PrintEngine: @unchecked Sendable {
    public init() {}

    public func run(_ request: PrintRequest, tools: ToolPaths, outputDirectory: URL? = nil, progress: @escaping @Sendable (String) -> Void = { _ in }) throws -> [PageResult] {
        if !request.dryRun && !FileManager.default.fileExists(atPath: tools.usb.path) {
            throw F11PrintError.missingResource("USB helper")
        }
        progress("Opening \(request.pdf.lastPathComponent)…")
        let pages = try PageRenderer.render(pdf: request.pdf, width: request.width, height: request.height, margin: request.margin, shiftX: request.shiftX, shiftY: request.shiftY)
        let root = outputDirectory ?? FileManager.default.temporaryDirectory.appendingPathComponent("F11PDF-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        var results: [PageResult] = []
        for (index, gray) in pages.enumerated() {
            let number = index + 1
            progress("Preparing page \(number) of \(pages.count)…")
            let base = root.appendingPathComponent(String(format: "page-%03d", number))
            let grayURL = base.appendingPathExtension("gray")
            let streamURL = base.appendingPathExtension("f11")
            let previewURL = base.appendingPathExtension("png")
            try Data(gray).write(to: grayURL)
            try writePreview(gray: gray, width: request.width, height: request.height, to: previewURL)
            let intendedRows = try F11JobEncoder.monochrome(gray: gray, width: request.width, height: request.height)
            let stream = try F11JobEncoder.encode(gray: gray, sourceWidth: request.width, sourceHeight: request.height, speed: request.speed, density: request.density, tracking: 0, copies: request.copies)
            let decoded = try F11JobDecoder.decode(stream)
            guard decoded.widthBytes == 199, decoded.height == request.height, decoded.rows == intendedRows else { throw F11PrintError.invalidStream }
            try stream.write(to: streamURL)
            if !request.dryRun {
                progress("Printing page \(number) of \(pages.count)…")
                try runProcess(tools.usb, [streamURL.path])
            }
            let digest = SHA256.hash(data: stream).map { String(format: "%02x", $0) }.joined()
            results.append(PageResult(page: number, streamBytes: stream.count, sha256: digest, printed: !request.dryRun, preview: previewURL.path))
        }
        progress(request.dryRun ? "Ready — dry run complete." : "Sent \(pages.count) page\(pages.count == 1 ? "" : "s") to the F11.")
        return results
    }

    private func runProcess(_ executable: URL, _ arguments: [String], environment: [String:String]? = nil, stdout: URL? = nil) throws {
        let p = Process(); p.executableURL = executable; p.arguments = arguments
        if let environment { p.environment = environment }
        let errors = Pipe(); p.standardError = errors
        var handle: FileHandle?
        if let stdout {
            FileManager.default.createFile(atPath: stdout.path, contents: nil)
            handle = try FileHandle(forWritingTo: stdout); p.standardOutput = handle
        }
        try p.run(); p.waitUntilExit(); try handle?.close()
        let message = String(data: errors.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
        guard p.terminationStatus == 0 else { throw F11PrintError.processFailed("\(executable.lastPathComponent) failed: \(message)") }
    }

    private func writePreview(gray: [UInt8], width: Int, height: Int, to url: URL) throws {
        guard let provider = CGDataProvider(data: Data(gray) as CFData),
              let image = CGImage(width: width, height: height, bitsPerComponent: 8, bitsPerPixel: 8, bytesPerRow: width, space: CGColorSpaceCreateDeviceGray(), bitmapInfo: CGBitmapInfo(rawValue: CGImageAlphaInfo.none.rawValue), provider: provider, decode: nil, shouldInterpolate: false, intent: .defaultIntent),
              let dest = CGImageDestinationCreateWithURL(url as CFURL, "public.png" as CFString, 1, nil) else { throw F11PrintError.processFailed("Could not write preview.") }
        CGImageDestinationAddImage(dest, image, nil)
        guard CGImageDestinationFinalize(dest) else { throw F11PrintError.processFailed("Could not finalize preview.") }
    }
}
