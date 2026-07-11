import AppKit
import UniformTypeIdentifiers
import F11PrintCore

@MainActor
final class DropView: NSView {
    var onPDF: ((URL) -> Void)?

    override init(frame: NSRect) {
        super.init(frame: frame)
        registerForDraggedTypes([.fileURL])
        wantsLayer = true
        layer?.cornerRadius = 18
        layer?.borderWidth = 2
        layer?.borderColor = NSColor.separatorColor.cgColor
    }

    required init?(coder: NSCoder) { fatalError() }
    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation { .copy }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        guard let string = sender.draggingPasteboard.string(forType: .fileURL),
              let url = URL(string: string),
              url.pathExtension.lowercased() == "pdf" else { return false }
        onPDF?(url)
        return true
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let window = NSWindow(
        contentRect: NSRect(x: 0, y: 0, width: 620, height: 440),
        styleMask: [.titled, .closable, .miniaturizable],
        backing: .buffered,
        defer: false
    )
    private let titleLabel = NSTextField(labelWithString: "Drop a PDF here")
    private let fileLabel = NSTextField(labelWithString: "One PDF. Beautifully fit to Letter. Printed directly to your F11.")
    private let status = NSTextField(labelWithString: "Printer ready")
    private let button = NSButton(title: "Choose PDF…", target: nil, action: nil)
    private var selected: URL?

    func applicationDidFinishLaunching(_ notification: Notification) {
        window.title = "F11 PDF Printer"
        window.center()
        window.isReleasedWhenClosed = false
        let view = DropView(frame: NSRect(x: 34, y: 78, width: 552, height: 320))
        window.contentView = view

        titleLabel.font = .systemFont(ofSize: 32, weight: .bold)
        titleLabel.alignment = .center
        titleLabel.frame = NSRect(x: 35, y: 220, width: 482, height: 48)
        view.addSubview(titleLabel)

        fileLabel.font = .systemFont(ofSize: 15)
        fileLabel.textColor = .secondaryLabelColor
        fileLabel.alignment = .center
        fileLabel.frame = NSRect(x: 45, y: 178, width: 462, height: 42)
        fileLabel.maximumNumberOfLines = 2
        view.addSubview(fileLabel)

        button.target = self
        button.action = #selector(action)
        button.bezelStyle = .rounded
        button.controlSize = .large
        button.frame = NSRect(x: 176, y: 115, width: 200, height: 44)
        view.addSubview(button)

        status.frame = NSRect(x: 55, y: 24, width: 442, height: 30)
        status.alignment = .center
        status.textColor = .secondaryLabelColor
        view.addSubview(status)

        view.onPDF = { [weak self] url in self?.choose(url) }
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func application(_ application: NSApplication, open urls: [URL]) {
        if let pdf = urls.first(where: { $0.pathExtension.lowercased() == "pdf" }) { choose(pdf) }
    }

    private func choose(_ url: URL) {
        selected = url
        titleLabel.stringValue = url.lastPathComponent
        fileLabel.stringValue = "Ready to print every page at calibrated 203 dpi."
        button.title = "Print PDF"
    }

    @objc private func action() {
        if selected == nil {
            let panel = NSOpenPanel()
            panel.allowedContentTypes = [.pdf]
            if panel.runModal() == .OK, let url = panel.url { choose(url) }
            return
        }
        guard let url = selected else { return }
        button.isEnabled = false
        status.stringValue = "Preparing…"
        let resources = Bundle.main.resourceURL!
        Task.detached { [weak self] in
            do {
                let request = try PrintRequest(pdf: url)
                let result = try PrintEngine().run(request, tools: ToolPaths(resourceDirectory: resources)) { message in
                    Task { @MainActor in self?.status.stringValue = message }
                }
                await MainActor.run {
                    self?.status.stringValue = "Sent \(result.count) page(s) to the F11"
                    self?.button.isEnabled = true
                }
            } catch {
                await MainActor.run {
                    self?.status.stringValue = error.localizedDescription
                    self?.button.isEnabled = true
                }
            }
        }
    }
}

func runCLI() throws {
    var arguments = Array(CommandLine.arguments.dropFirst())
    var dryRun = false, density = 8, speed = 16, copies = 1
    var output: URL?, pdf: URL?
    while !arguments.isEmpty {
        let argument = arguments.removeFirst()
        switch argument {
        case "--dry-run": dryRun = true
        case "--density":
            guard let value = arguments.first, let parsed = Int(value), (1...15).contains(parsed) else { throw F11PrintError.invalidOption("Density must be between 1 and 15.") }
            density = parsed; arguments.removeFirst()
        case "--speed":
            guard let value = arguments.first, let parsed = Int(value), parsed > 0 else { throw F11PrintError.invalidOption("Speed must be a positive integer.") }
            speed = parsed; arguments.removeFirst()
        case "--copies":
            guard let value = arguments.first, let parsed = Int(value), (1...255).contains(parsed) else { throw F11PrintError.invalidOption("Copies must be between 1 and 255.") }
            copies = parsed; arguments.removeFirst()
        case "--output":
            guard let value = arguments.first else { throw F11PrintError.invalidOption("--output requires a directory.") }
            output = URL(fileURLWithPath: value); arguments.removeFirst()
        case "--help", "-h":
            print("Usage: f11print [--dry-run] [--density 1-15] [--speed N] [--copies N] [--output DIR] PDF")
            return
        default: pdf = URL(fileURLWithPath: argument)
        }
    }
    guard let pdf else { throw F11PrintError.notPDF }
    var request = try PrintRequest(pdf: pdf)
    request.dryRun = dryRun
    request.density = density
    request.speed = speed
    request.copies = copies
    let executable = URL(fileURLWithPath: CommandLine.arguments[0]).resolvingSymlinksInPath()
    var resources = executable.deletingLastPathComponent().deletingLastPathComponent().appendingPathComponent("Resources")
    if !FileManager.default.fileExists(atPath: resources.appendingPathComponent("f11usb").path),
       let bundleResources = Bundle.main.resourceURL {
        resources = bundleResources
    }
    let result = try PrintEngine().run(request, tools: ToolPaths(resourceDirectory: resources), outputDirectory: output) {
        fputs("\($0)\n", stderr)
    }
    print(String(data: try JSONEncoder().encode(result), encoding: .utf8)!)
}

if CommandLine.arguments.count > 1 {
    do { try runCLI() }
    catch { fputs("f11print: \(error.localizedDescription)\n", stderr); exit(1) }
} else {
    let app = NSApplication.shared
    let delegate = AppDelegate()
    app.delegate = delegate
    app.setActivationPolicy(.regular)
    app.run()
}
