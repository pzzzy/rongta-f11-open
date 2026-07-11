// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "RongtaF11",
    platforms: [.macOS(.v13)],
    products: [
        .library(name: "F11PrintCore", targets: ["F11PrintCore"]),
        .executable(name: "f11print", targets: ["F11Print"]),
    ],
    targets: [
        .target(
            name: "F11PrintCore",
            linkerSettings: [
                .linkedFramework("AppKit"),
                .linkedFramework("PDFKit"),
            ]
        ),
        .executableTarget(
            name: "F11Print",
            dependencies: ["F11PrintCore"],
            linkerSettings: [
                .linkedFramework("AppKit"),
                .linkedFramework("PDFKit"),
            ]
        ),
        .executableTarget(name: "F11CoreTests", dependencies: ["F11PrintCore"]),
    ]
)
