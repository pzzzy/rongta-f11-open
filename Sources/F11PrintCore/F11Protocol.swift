import Foundation

public extension Data {
    var hex: String { map { String(format: "%02x", $0) }.joined() }
    mutating func appendLE(_ value: UInt16) { append(UInt8(value & 0xff)); append(UInt8(value >> 8)) }
    mutating func appendLE(_ value: UInt32) { append(UInt8(value & 0xff)); append(UInt8((value >> 8) & 0xff)); append(UInt8((value >> 16) & 0xff)); append(UInt8(value >> 24)) }
}

public enum F11CodecError: Error { case invalid(String) }

public enum F11CRC {
    public static func checksum(_ data: Data, seed: UInt32 = 0x76953521) -> UInt32 {
        var crc = seed ^ 0xffffffff
        for byte in data {
            crc ^= UInt32(byte)
            for _ in 0..<8 { crc = (crc & 1) != 0 ? (crc >> 1) ^ 0xedb88320 : crc >> 1 }
        }
        return crc ^ 0xffffffff
    }
}

public struct F11Frame: Sendable {
    public let appClass, command, subcommand: UInt8
    public let payload: Data
    public init(appClass: UInt8, command: UInt8, subcommand: UInt8, payload: Data) { self.appClass=appClass; self.command=command; self.subcommand=subcommand; self.payload=payload }
    public var encoded: Data {
        var body=Data([appClass,command,subcommand]); body.appendLE(UInt16(payload.count)); body.append(payload)
        var out=Data([0xa3,0x1e,0x1c,0]); out.appendLE(UInt16(body.count)); out.append(body); out.appendLE(F11CRC.checksum(body)); return out
    }
}

public final class F11Huffman: @unchecked Sendable {
    public final class Node {
        let symbol: UInt16; let weight: Int; let order: Int; var left: Node?; var right: Node?
        init(_ symbol: UInt16,_ weight: Int,_ order: Int,_ left: Node?=nil,_ right: Node?=nil){self.symbol=symbol;self.weight=weight;self.order=order;self.left=left;self.right=right}
        var leaf: Bool { left == nil && right == nil }
    }
    public let root: Node
    private let codes: [[UInt8]]
    public let nodeCount: Int
    fileprivate init(root: Node) {
        self.root=root; var c=[[UInt8]](repeating:[],count:256); var n=0
        func walk(_ x:Node,_ bits:[UInt8]) { n += 1; if x.leaf { if x.symbol < 256 { c[Int(x.symbol)] = bits.isEmpty ? [0] : bits }; return }; walk(x.left!,bits+[0]); walk(x.right!,bits+[1]) }
        walk(root,[]); codes=c; nodeCount=n
    }
    public static func build(from data: Data) -> F11Huffman {
        var counts=[Int](repeating:0,count:256); for b in data { counts[Int(b)] += 1 }
        var nodes=[Node](); var order=0
        for i in 0..<256 where counts[i] > 0 { nodes.append(Node(UInt16(i),counts[i],order));order += 1 }
        if nodes.count == 1 { let dummy = Int(nodes[0].symbol) == 0 ? 1 : 0; nodes.append(Node(UInt16(dummy),0,order)); order += 1 }
        if nodes.isEmpty { nodes=[Node(0,1,0),Node(1,0,1)]; order=2 }
        var synthetic: UInt16=256
        while nodes.count > 1 {
            nodes.sort { $0.weight != $1.weight ? $0.weight < $1.weight : $0.order < $1.order }
            let a=nodes.removeFirst(),b=nodes.removeFirst(); nodes.append(Node(synthetic,a.weight+b.weight,order,a,b)); synthetic &+= 1; order += 1
        }
        return F11Huffman(root:nodes[0])
    }
    public var serializedTraversals: Data {
        var pre=[UInt16](),mid=[UInt16](); func p(_ n:Node){pre.append(n.symbol);if let l=n.left{p(l)};if let r=n.right{p(r)}}; func m(_ n:Node){if let l=n.left{m(l)};mid.append(n.symbol);if let r=n.right{m(r)}};p(root);m(root)
        var d=Data(); for x in pre+mid { d.appendLE(x) }; return d
    }
    public func encode(_ data: Data) -> (bytes: Data,paddingBits: UInt8) {
        var bits=[UInt8](); for b in data { bits.append(contentsOf:codes[Int(b)]) }; let pad=(8-bits.count%8)%8; bits.append(contentsOf:repeatElement(1,count:pad)); var out=Data()
        for i in stride(from:0,to:bits.count,by:8){var v:UInt8=0;for x in bits[i..<i+8]{v=(v<<1)|x};out.append(v)}
        return (out,UInt8(pad))
    }
    public func decode(_ bytes: Data,paddingBits: UInt8,count: Int) -> Data {
        guard paddingBits <= 7, count >= 0, bytes.count * 8 >= Int(paddingBits) else { return Data() }
        var out=Data(),node=root; let valid=bytes.count*8-Int(paddingBits); var seen=0
        for byte in bytes {
            for bit in stride(from:7,through:0,by:-1) {
                if seen >= valid { break }
                seen += 1
                guard let next = ((byte>>bit)&1)==0 ? node.left : node.right else { return Data() }
                node = next
                if node.leaf {
                    guard node.symbol < 256 else { return Data() }
                    out.append(UInt8(node.symbol)); node=root
                }
            }
        }
        guard node === root, out.count == count else { return Data() }
        return out
    }
}

public struct F11DecodedJob { public let widthBytes:Int; public let height:Int; public let rows:[Data] }

public enum F11JobEncoder {
    static func frame(_ c:UInt8,_ s:UInt8,_ p:Data=Data(), app:UInt8=0x11)->Data { F11Frame(appClass:app,command:c,subcommand:s,payload:p).encoded }
    public static func monochrome(gray:[UInt8],width:Int,height:Int) throws -> [Data] {
        guard width >= 1592, height > 0, height <= 65535,
              gray.count == width*height else { throw F11CodecError.invalid("grayscale dimensions") }
        let outputWidth=1592, bytes=199, x0=max(0,(width-outputWidth)/2); var rows=[Data](); rows.reserveCapacity(height)
        // Ordered Bayer avoids error-diffusion state and gives stable text/photo output.
        let bayer=[[0,8,2,10],[12,4,14,6],[3,11,1,9],[15,7,13,5]]
        for y in 0..<height { var row=Data(repeating:0,count:bytes); for x in 0..<outputWidth { let sx=min(width-1,x+x0); let threshold=bayer[y&3][x&3]*16+8; if Int(gray[y*width+sx]) < threshold { row[x>>3] |= UInt8(0x80>>(x&7)) } }; rows.append(row) }
        return rows
    }
    public static func rowFrame(index:Int,row:Data,tree:F11Huffman)->Data {
        let e=tree.encode(row); var p=Data();p.appendLE(UInt16(index));p.appendLE(UInt16(8+e.bytes.count));p.append(contentsOf:[0x11,UInt8(row.count),0,0,0,0,0,e.paddingBits]);p.append(e.bytes);return frame(5,0x0d,p,app:0x10)
    }
    public static func encode(gray:[UInt8],sourceWidth:Int,sourceHeight:Int,speed:Int,density:Int,tracking:Int,copies:Int = 1) throws -> Data {
        guard (1...255).contains(speed), (1...15).contains(density),
              (0...255).contains(tracking), (1...255).contains(copies) else {
            throw F11CodecError.invalid("print settings")
        }
        let rows=try monochrome(gray:gray,width:sourceWidth,height:sourceHeight); var all=Data();for r in rows{all.append(r)};let tree=F11Huffman.build(from:all);var out=Data()
        out.append(frame(5,0x11,Data([UInt8(tracking)])));out.append(frame(5,7,Data([UInt8(speed)])));out.append(frame(5,4,Data([3,UInt8(density),8])))
        var start=Data();start.appendLE(UInt16(1592));start.appendLE(UInt16(sourceHeight));start.append(contentsOf:[0,0,1]);out.append(frame(5,0x0b,start));out.append(frame(5,0x0e,tree.serializedTraversals))
        // Vendor-compatible transfer sentinel.
        var sentinel=Data();sentinel.appendLE(UInt16(0));sentinel.appendLE(UInt16(8));sentinel.append(contentsOf:[0x11,199,0,0,0,0,0,1]);out.append(frame(5,0x0d,sentinel,app:0x10))
        for (i,row) in rows.enumerated(){out.append(rowFrame(index:i+1,row:row,tree:tree))}
        guard (1...255).contains(copies) else { throw F11CodecError.invalid("copies") }
        out.append(frame(5,0x0c));out.append(frame(5,8,Data([UInt8(copies),0x13,0])));return out
    }
}

public enum F11JobDecoder {
    static func frames(_ data:Data) throws -> [F11Frame] { var result=[F11Frame](),i=0;while i<data.count{guard i+15<=data.count,Array(data[i..<i+3]) == [0xa3,0x1e,0x1c] else{throw F11CodecError.invalid("sync")};let n=Int(data[i+4])|Int(data[i+5])<<8;let end=i+6+n+4;guard end<=data.count else{throw F11CodecError.invalid("truncated")};let body=data.subdata(in:i+6..<i+6+n);let crc=UInt32(data[end-4])|UInt32(data[end-3])<<8|UInt32(data[end-2])<<16|UInt32(data[end-1])<<24;guard F11CRC.checksum(body)==crc else{throw F11CodecError.invalid("CRC")};let plen=Int(body[3])|Int(body[4])<<8;guard plen==body.count-5 else{throw F11CodecError.invalid("length")};result.append(F11Frame(appClass:body[0],command:body[1],subcommand:body[2],payload:body.subdata(in:5..<body.count)));i=end};return result }
    public static func decodeRow(_ encoded:Data,tree:F11Huffman)->Data {
        guard let f=try? frames(encoded).first, f.payload.count >= 12 else{return Data()}
        let p=f.payload,pad=p[11]
        return tree.decode(p.subdata(in:12..<p.count),paddingBits:pad,count:Int(p[5]))
    }
    public static func decode(_ data:Data) throws -> F11DecodedJob {
        let fs=try frames(data)
        guard let start=fs.first(where:{$0.subcommand==0x0b}), start.payload.count >= 4,
              let tf=fs.first(where:{$0.subcommand==0x0e}) else{throw F11CodecError.invalid("missing setup")}
        let w=Int(start.payload[0])|Int(start.payload[1])<<8,h=Int(start.payload[2])|Int(start.payload[3])<<8
        guard w == 1592, h > 0 else { throw F11CodecError.invalid("geometry") }
        let tree=try treeFrom(start:tf.payload)
        let rfs=Array(fs.filter{$0.appClass==0x10 && $0.subcommand==0x0d}.dropFirst())
        guard rfs.count == h else { throw F11CodecError.invalid("row count") }
        let rows=try rfs.map{f->Data in
            let p=f.payload
            guard p.count >= 12, Int(p[5]) == w / 8 else { throw F11CodecError.invalid("row header") }
            let row=tree.decode(p.subdata(in:12..<p.count),paddingBits:p[11],count:Int(p[5]))
            guard row.count == w / 8 else { throw F11CodecError.invalid("row data") }
            return row
        }
        return F11DecodedJob(widthBytes:w/8,height:h,rows:rows)
    }
    static func treeFrom(start data:Data)throws->F11Huffman {
        guard data.count % 4 == 0, !data.isEmpty else { throw F11CodecError.invalid("tree size") }
        let n=data.count/4
        func word(_ offset:Int)->UInt16 { UInt16(data[offset]) | UInt16(data[offset+1])<<8 }
        let pre=(0..<n).map{word($0*2)}, mid=(0..<n).map{word(n*2+$0*2)};var pi=0,order=0
        func build(_ lo:Int,_ hi:Int)throws->F11Huffman.Node {
            guard lo<hi,pi<n else{throw F11CodecError.invalid("tree traversal")};let s=pre[pi];pi += 1;guard let split=mid[lo..<hi].firstIndex(of:s) else{throw F11CodecError.invalid("tree symbol")};let node=F11Huffman.Node(s,0,order);order += 1
            if split>lo{node.left=try build(lo,split)};if split+1<hi{node.right=try build(split+1,hi)};return node
        }
        let root=try build(0,n);guard pi==n else{throw F11CodecError.invalid("unused tree nodes")};return F11Huffman(root:root)
    }
}
