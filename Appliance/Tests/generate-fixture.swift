import Foundation
import F11PrintCore

let output=URL(fileURLWithPath:CommandLine.arguments[1])
let width=1592,height=8
var gray=[UInt8](repeating:255,count:width*height)
for y in 0..<height { for x in 0..<width where (x+y)%17==0 { gray[y*width+x]=0 } }
let stream=try F11JobEncoder.encode(gray:gray,sourceWidth:width,sourceHeight:height,speed:16,density:8,tracking:0)
try stream.write(to:output)
print(stream.count)
