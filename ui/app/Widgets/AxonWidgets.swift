import SwiftUI
import WidgetKit

@main
struct AxonWidgets: WidgetBundle {
    var body: some Widget {
        GamesWidget()
        CallActivityWidget()
    }
}

enum WidgetPalette {
    static let primary = Color(red: 0x59 / 255, green: 0x45 / 255, blue: 0xB9 / 255)
    static let accent = Color(red: 0xAD / 255, green: 0x9F / 255, blue: 0xFC / 255)
    static let success = Color(red: 0x72 / 255, green: 0xCE / 255, blue: 0x9F / 255)
    static let destructive = Color(red: 0xF0 / 255, green: 0x7F / 255, blue: 0x91 / 255)
}

struct WidgetGame: Identifiable {
    let id: String
    let title: String
    let symbol: String

    static let all = [
        WidgetGame(id: "chess", title: "Chess", symbol: "crown"),
        WidgetGame(id: "checkers", title: "Checkers", symbol: "smallcircle.filled.circle"),
        WidgetGame(id: "quoridor", title: "Quoridor", symbol: "square.grid.3x3"),
        WidgetGame(id: "tictactoe", title: "Tic-Tac-Toe", symbol: "number"),
        WidgetGame(id: "durak", title: "Durak", symbol: "suit.spade.fill"),
    ]

    var quickMatch: URL { URL(string: "axon://quick/\(id)")! }
    var practice: URL { URL(string: "axon://practice/\(id)")! }
}

struct GamesEntry: TimelineEntry {
    let date: Date
}

struct GamesProvider: TimelineProvider {
    func placeholder(in context: Context) -> GamesEntry { GamesEntry(date: .now) }

    func getSnapshot(in context: Context, completion: @escaping (GamesEntry) -> Void) {
        completion(GamesEntry(date: .now))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<GamesEntry>) -> Void) {
        completion(Timeline(entries: [GamesEntry(date: .now)], policy: .never))
    }
}

struct GamesWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "dev.axon.games", provider: GamesProvider()) { _ in
            GamesWidgetView()
                .containerBackground(for: .widget) { Color(.systemBackground) }
        }
        .configurationDisplayName("Quick match")
        .description("Jump into a rated game or practice offline.")
        .supportedFamilies([.systemSmall, .systemMedium, .accessoryCircular])
    }
}

struct GamesWidgetView: View {
    @Environment(\.widgetFamily) private var family

    var body: some View {
        switch family {
        case .accessoryCircular:
            ZStack {
                AccessoryWidgetBackground()
                Image(systemName: "bolt.fill").font(.title3)
            }
            .widgetURL(URL(string: "axon://quick/chess"))
        case .systemSmall:
            VStack(alignment: .leading, spacing: 6) {
                Image(systemName: "bolt.fill")
                    .font(.title2)
                    .foregroundStyle(WidgetPalette.primary)
                Spacer()
                Text("Quick match").font(.headline)
                Text("Chess · rated").font(.caption).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            .widgetURL(URL(string: "axon://quick/chess"))
        default:
            VStack(alignment: .leading, spacing: 8) {
                Text("Quick match").font(.headline)
                HStack(spacing: 8) {
                    ForEach(WidgetGame.all.prefix(4)) { game in
                        Link(destination: game.quickMatch) {
                            VStack(spacing: 6) {
                                Image(systemName: game.symbol)
                                    .font(.title3)
                                    .frame(width: 44, height: 44)
                                    .background(WidgetPalette.primary.opacity(0.14), in: .rect(cornerRadius: 12))
                                    .foregroundStyle(WidgetPalette.primary)
                                Text(game.title).font(.caption2).lineLimit(1)
                            }
                            .frame(maxWidth: .infinity)
                        }
                    }
                }
                Link(destination: URL(string: "axon://tournaments")!) {
                    Label("Tournaments", systemImage: "trophy").font(.caption)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
    }
}
