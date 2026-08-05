// onIT macOS widget: renders the state snapshot the app writes to
// ~/Library/Application Support/onIT/widget-state.json (see
// cmd/onit/widgetstate_darwin.go). Tapping opens onit://open, which the
// running app answers by showing its window.
//
// Built by `make widget` with plain swiftc — no Xcode project. The bundle
// layout and signing live in the Makefile.

import SwiftUI
import WidgetKit

struct StateEntry: TimelineEntry {
    let date: Date
    let label: String
    let color: Color
    let connected: Bool
    let stale: Bool
}

struct AppState: Decodable {
    let label: String
    let color: String
    let connected: Bool
    let updatedAt: Date
}

// The sandbox home is the widget's own container; the state file lives in the
// real user home, which the entitlements open read-only. getpwuid gives the
// real home regardless of sandboxing.
func realHome() -> String {
    if let pw = getpwuid(getuid()), let dir = pw.pointee.pw_dir {
        return String(cString: dir)
    }
    return NSHomeDirectory()
}

func loadEntry(now: Date) -> StateEntry {
    let path = realHome() + "/Library/Application Support/onIT/widget-state.json"
    let offline = StateEntry(
        date: now, label: "Offline", color: .gray, connected: false, stale: true)
    let dec = JSONDecoder()
    dec.dateDecodingStrategy = .iso8601 // Go writes whole seconds for this
    // A snapshot nobody refreshed for half an hour means the app is gone;
    // show that rather than a stale presence lying about a call.
    guard let data = FileManager.default.contents(atPath: path),
          let st = try? dec.decode(AppState.self, from: data),
          now.timeIntervalSince(st.updatedAt) <= 30 * 60
    else { return offline }
    return StateEntry(date: now, label: st.label, color: Color(hex: st.color),
                      connected: st.connected, stale: false)
}

extension Color {
    init(hex: String) {
        var v: UInt64 = 0
        Scanner(string: String(hex.dropFirst())).scanHexInt64(&v)
        self.init(
            red: Double((v >> 16) & 0xFF) / 255,
            green: Double((v >> 8) & 0xFF) / 255,
            blue: Double(v & 0xFF) / 255)
    }
}

struct Provider: TimelineProvider {
    func placeholder(in _: Context) -> StateEntry {
        StateEntry(date: .now, label: "Available", color: Color(hex: "#90C450"),
                   connected: true, stale: false)
    }
    func getSnapshot(in _: Context, completion: @escaping (StateEntry) -> Void) {
        completion(loadEntry(now: .now))
    }
    func getTimeline(in _: Context, completion: @escaping (Timeline<StateEntry>) -> Void) {
        // The app pushes a reload on every change (onit-widgetreload); the
        // 15-minute repeat is only the backstop that eventually flips the
        // widget to Offline after the app quits.
        let entry = loadEntry(now: .now)
        completion(Timeline(entries: [entry], policy: .after(.now + 15 * 60)))
    }
}

struct StateView: View {
    var entry: StateEntry
    @Environment(\.widgetFamily) var family

    var body: some View {
        Group {
            if family == .systemMedium {
                HStack(spacing: 14) {
                    dot(size: 44)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(entry.label).font(.title3).bold()
                        Text(subtitle).font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer()
                }
                .padding()
            } else {
                VStack(spacing: 8) {
                    dot(size: 40)
                    Text(entry.label).font(.headline).multilineTextAlignment(.center)
                    Text(subtitle).font(.caption2).foregroundStyle(.secondary)
                }
            }
        }
        .widgetURL(URL(string: "onit://open"))
    }

    var subtitle: String {
        if entry.stale { return "onIT not running" }
        return entry.connected ? "light connected" : "no light"
    }

    func dot(size: CGFloat) -> some View {
        Circle()
            .fill(entry.color)
            .frame(width: size, height: size)
            .overlay(Circle().stroke(.quaternary, lineWidth: 1))
            .opacity(entry.stale ? 0.4 : 1)
    }
}

struct OnITWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "casa.baillargeon.onit.state", provider: Provider()) {
            StateView(entry: $0)
                .containerBackground(.background, for: .widget)
        }
        .configurationDisplayName("onIT status")
        .description("Current busylight state; tap to open onIT.")
        .supportedFamilies([.systemSmall, .systemMedium])
    }
}

@main
struct OnITWidgetBundle: WidgetBundle {
    var body: some Widget {
        OnITWidget()
    }
}
