import AppKit
import Foundation

let args=CommandLine.arguments
guard args.count==2 else{exit(2)}
let image=NSImage(size:NSSize(width:400,height:300))
image.lockFocus()
NSColor.white.setFill();NSRect(x:0,y:0,width:400,height:300).fill()
NSColor.black.setStroke();let border=NSBezierPath(rect:NSRect(x:40,y:40,width:320,height:220));border.lineWidth=8;border.stroke()
("F11 PNG" as NSString).draw(at:NSPoint(x:100,y:130),withAttributes:[.font:NSFont.boldSystemFont(ofSize:36),.foregroundColor:NSColor.black])
image.unlockFocus()
guard let tiff=image.tiffRepresentation,let rep=NSBitmapImageRep(data:tiff),let png=rep.representation(using:.png,properties:[:]) else{exit(1)}
try png.write(to:URL(fileURLWithPath:args[1]))
