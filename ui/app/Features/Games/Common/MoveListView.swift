import SwiftUI

enum MoveListLayout: Sendable, Equatable {
    case pairs
    case plain

    init(_ kind: GameKind) {
        self = kind == .chess || kind == .checkers ? .pairs : .plain
    }
}

struct MoveListEntry: Hashable, Sendable {
    let ply: Int
    let text: String
}

struct MoveListRow: Identifiable, Hashable, Sendable {
    let id: Int
    let number: Int
    let entries: [MoveListEntry]

    static func rows(_ moves: [String], layout: MoveListLayout) -> [MoveListRow] {
        let entries = moves.enumerated().map { MoveListEntry(ply: $0.offset + 1, text: $0.element) }
        switch layout {
        case .plain:
            return entries.map { MoveListRow(id: $0.ply - 1, number: $0.ply, entries: [$0]) }
        case .pairs:
            return stride(from: 0, to: entries.count, by: 2).map { start in
                MoveListRow(id: start / 2, number: start / 2 + 1, entries: Array(entries[start..<min(start + 2, entries.count)]))
            }
        }
    }

    static func rowID(ply: Int, layout: MoveListLayout) -> Int? {
        guard ply > 0 else { return nil }
        switch layout {
        case .plain: return ply - 1
        case .pairs: return (ply - 1) / 2
        }
    }

    static func scrollTarget(moveCount: Int, highlightedPly: Int?, layout: MoveListLayout) -> Int? {
        guard moveCount > 0 else { return nil }
        guard let highlightedPly else { return rowID(ply: moveCount, layout: layout) }
        return rowID(ply: min(highlightedPly, moveCount), layout: layout) ?? 0
    }
}

struct MoveListView: View {
    private let moves: [String]
    private let highlightedPly: Int?
    private let layout: MoveListLayout

    init(moves: [String], highlightedPly: Int?, layout: MoveListLayout = .pairs) {
        self.moves = moves
        self.highlightedPly = highlightedPly
        self.layout = layout
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                MoveListContent(moves: moves, highlightedPly: highlightedPly, layout: layout)
            }
            .onAppear { scroll(proxy, animated: false) }
            .onChange(of: highlightedPly) { scroll(proxy, animated: true) }
            .onChange(of: moves.count) { scroll(proxy, animated: true) }
        }
    }

    private func scroll(_ proxy: ScrollViewProxy, animated: Bool) {
        guard let target = MoveListRow.scrollTarget(moveCount: moves.count, highlightedPly: highlightedPly, layout: layout) else { return }
        if animated {
            withAnimation(.easeOut(duration: 0.2)) { proxy.scrollTo(target, anchor: .center) }
        } else {
            proxy.scrollTo(target, anchor: .center)
        }
    }
}

struct MoveListContent: View {
    let moves: [String]
    let highlightedPly: Int?
    let layout: MoveListLayout

    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        let rows = MoveListRow.rows(moves, layout: layout)
        if rows.isEmpty {
            Text("No moves yet")
                .font(.axCallout)
                .foregroundStyle(Theme.muted)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(12)
        } else {
            LazyVStack(alignment: .leading, spacing: 2) {
                ForEach(rows) { row in
                    rowView(row)
                        .id(row.id)
                }
            }
            .padding(8)
        }
    }

    private func rowView(_ row: MoveListRow) -> some View {
        HStack(spacing: 4) {
            Text("\(row.number).")
                .foregroundStyle(Theme.muted)
                .frame(width: 30, alignment: .leading)
            ForEach(row.entries, id: \.ply) { entry in
                entryView(entry)
            }
            if layout == .pairs && row.entries.count == 1 {
                Color.clear.frame(maxWidth: .infinity, maxHeight: 1)
            }
        }
        .font(.axMono(15, relativeTo: .callout))
        .accessibilityElement(children: .combine)
    }

    private func entryView(_ entry: MoveListEntry) -> some View {
        let highlighted = entry.ply == highlightedPly
        let palette = BoardPalette.resolve(colorScheme)
        return Text(entry.text)
            .fontWeight(highlighted ? .semibold : .regular)
            .foregroundStyle(highlighted ? AnyShapeStyle(palette.onAccent) : AnyShapeStyle(.primary))
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background {
                if highlighted {
                    RoundedRectangle(cornerRadius: 6, style: .continuous).fill(palette.violet)
                }
            }
            .accessibilityAddTraits(highlighted ? .isSelected : [])
    }
}

struct MoveStrip: View {
    let moves: [String]
    let highlightedPly: Int?
    let layout: MoveListLayout

    var body: some View {
        let rows = MoveListRow.rows(moves, layout: layout)
        ScrollViewReader { proxy in
            ScrollView(.horizontal) {
                HStack(spacing: 10) {
                    ForEach(rows) { row in
                        HStack(spacing: 3) {
                            Text("\(row.number).")
                                .foregroundStyle(Theme.muted)
                            ForEach(row.entries, id: \.ply) { entry in
                                chip(entry)
                            }
                        }
                        .id(row.id)
                        .accessibilityElement(children: .combine)
                    }
                }
                .font(.axMono(14, relativeTo: .subheadline))
                .padding(.horizontal, 14)
                .padding(.vertical, 12)
            }
            .scrollIndicators(.hidden)
            .onAppear { scroll(proxy, animated: false) }
            .onChange(of: highlightedPly) { scroll(proxy, animated: true) }
            .onChange(of: moves.count) { scroll(proxy, animated: true) }
        }
        .cardBackground()
        .accessibilityIdentifier(AccessibilityID.Game.moves)
    }

    private func chip(_ entry: MoveListEntry) -> some View {
        let highlighted = entry.ply == highlightedPly
        return Text(entry.text)
            .fontWeight(highlighted ? .semibold : .regular)
            .foregroundStyle(highlighted ? AnyShapeStyle(.white) : AnyShapeStyle(.primary))
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(highlighted ? AnyShapeStyle(Color.accentColor) : AnyShapeStyle(.clear), in: .capsule)
            .accessibilityAddTraits(highlighted ? .isSelected : [])
    }

    private func scroll(_ proxy: ScrollViewProxy, animated: Bool) {
        guard let target = MoveListRow.scrollTarget(moveCount: moves.count, highlightedPly: highlightedPly, layout: layout) else { return }
        if animated {
            withAnimation(.snappy) { proxy.scrollTo(target, anchor: .center) }
        } else {
            proxy.scrollTo(target, anchor: .center)
        }
    }
}
