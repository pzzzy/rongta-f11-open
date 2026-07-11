import AppKit
import Foundation

let args = CommandLine.arguments
guard args.count == 3, let count = Int(args[2]), count > 0 else { exit(2) }
let url = URL(fileURLWithPath: args[1])
let data = NSMutableData()
guard let consumer = CGDataConsumer(data: data as CFMutableData),
      let context = CGContext(consumer: consumer, mediaBox: nil, nil) else { exit(1) }
for page in 1...count {
    var box = CGRect(x: 0, y: 0, width: 612, height: 792)
    context.beginPDFPage([kCGPDFContextMediaBox: NSData(bytes: &box, length: MemoryLayout<CGRect>.size)] as CFDictionary)
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(cgContext: context, flipped: false)
    NSColor.white.setFill(); box.fill()
    NSColor.black.setFill(); NSRect(x: 72, y: 580, width: 468, height: 4).fill()
    ("F11 AirPrint page \(page)" as NSString).draw(
        at: NSPoint(x: 72, y: 650),
        withAttributes: [.font: NSFont.boldSystemFont(ofSize: 36), .foregroundColor: NSColor.black]
    )
    NSGraphicsContext.restoreGraphicsState()
    context.endPDFPage()
}
context.closePDF()
try data.write(to: url)
