import Foundation
import CoreGraphics
import PDFKit

public enum F11PrintError: LocalizedError {
    case notPDF
    case cannotOpenPDF
    case noPages
    case missingResource(String)
    case processFailed(String)
    case invalidStream
    case cancelled
    case invalidOption(String)

    public var errorDescription: String? {
        switch self {
        case .notPDF: return "Choose a PDF file."
        case .cannotOpenPDF: return "The PDF could not be opened."
        case .noPages: return "The PDF has no printable pages."
        case .missingResource(let name): return "Required printer component is missing: \(name)"
        case .processFailed(let message): return message
        case .invalidStream: return "The Rongta filter produced an unexpected stream."
        case .cancelled: return "Printing was cancelled."
        case .invalidOption(let message): return message
        }
    }
}

public struct PrintRequest: Sendable {
    public let pdf: URL
    public var width = 1664
    public var height = 2233
    public var shiftX = -24
    public var shiftY = 0
    public var density = 8
    public var speed = 16
    public var margin = 72
    public var copies = 1
    public var dryRun = false

    public init(pdf: URL) throws {
        guard pdf.pathExtension.lowercased() == "pdf" else { throw F11PrintError.notPDF }
        self.pdf = pdf
    }
}

public enum PageRenderer {
    public struct OrientationDecision: Sendable {
        public let rotate: Bool
        public let rect: CGRect
    }

    public static func fitRect(source: CGSize, canvas: CGSize, margin: CGFloat) -> CGRect {
        let available = CGSize(width: max(1, canvas.width - 2 * margin), height: max(1, canvas.height - 2 * margin))
        let scale = min(available.width / source.width, available.height / source.height)
        let size = CGSize(width: source.width * scale, height: source.height * scale)
        return CGRect(x: (canvas.width - size.width) / 2, y: (canvas.height - size.height) / 2, width: size.width, height: size.height)
    }

    public static func orientationDecision(source: CGSize, canvas: CGSize, margin: CGFloat) -> OrientationDecision {
        let normal = fitRect(source: source, canvas: canvas, margin: margin)
        let rotated = fitRect(source: CGSize(width: source.height, height: source.width), canvas: canvas, margin: margin)
        return rotated.width * rotated.height > normal.width * normal.height
            ? OrientationDecision(rotate: true, rect: rotated)
            : OrientationDecision(rotate: false, rect: normal)
    }

    public static func render(pdf url: URL, width: Int, height: Int, margin: Int, shiftX: Int, shiftY: Int) throws -> [[UInt8]] {
        guard let document = PDFDocument(url: url) else { throw F11PrintError.cannotOpenPDF }
        guard document.pageCount > 0 else { throw F11PrintError.noPages }
        var output: [[UInt8]] = []
        let colorSpace = CGColorSpaceCreateDeviceGray()
        for index in 0..<document.pageCount {
            guard let page = document.page(at: index) else { continue }
            let bounds = page.bounds(for: .cropBox)
            let decision = orientationDecision(source: bounds.size, canvas: CGSize(width: width, height: height), margin: CGFloat(margin))
            var pixels = [UInt8](repeating: 255, count: width * height)
            let ok = pixels.withUnsafeMutableBytes { raw -> Bool in
                guard let base = raw.baseAddress,
                      let context = CGContext(data: base, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width, space: colorSpace, bitmapInfo: CGImageAlphaInfo.none.rawValue) else { return false }
                context.setFillColor(gray: 1, alpha: 1)
                context.fill(CGRect(x: 0, y: 0, width: width, height: height))
                context.interpolationQuality = .high
                context.saveGState()
                if decision.rotate {
                    context.translateBy(x: decision.rect.midX, y: decision.rect.midY)
                    context.rotate(by: .pi / 2)
                    let scale = min(decision.rect.height / bounds.width, decision.rect.width / bounds.height)
                    context.scaleBy(x: scale, y: scale)
                    context.translateBy(x: -bounds.midX, y: -bounds.midY)
                } else {
                    let scale = min(decision.rect.width / bounds.width, decision.rect.height / bounds.height)
                    context.translateBy(x: decision.rect.midX, y: decision.rect.midY)
                    context.scaleBy(x: scale, y: scale)
                    context.translateBy(x: -bounds.midX, y: -bounds.midY)
                }
                page.draw(with: .cropBox, to: context)
                context.restoreGState()
                return true
            }
            guard ok else { throw F11PrintError.processFailed("Could not allocate the page raster.") }
            output.append(translateGray(pixels, width: width, height: height, dx: shiftX, dy: shiftY))
        }
        guard !output.isEmpty else { throw F11PrintError.noPages }
        return output
    }

    public static func translateGray(_ source: [UInt8], width: Int, height: Int, dx: Int, dy: Int) -> [UInt8] {
        precondition(source.count == width * height)
        var output = [UInt8](repeating: 255, count: source.count)
        for y in 0..<height {
            let ny = y + dy
            guard ny >= 0 && ny < height else { continue }
            for x in 0..<width {
                let nx = x + dx
                guard nx >= 0 && nx < width else { continue }
                output[ny * width + nx] = source[y * width + x]
            }
        }
        return output
    }
}
