import SwiftUI

enum DurakPalette {
    static let felt = Color(light: 0xE9E5F8, dark: 0x1D2131)
    static let face = Color(light: 0xFFFFFF, dark: 0xF6F4FD)
    static let ink = Color(light: 0x25283A, dark: 0x25283A)
    static let red = Color(light: 0xB8324C, dark: 0xB8324C)
    static let back = Color(light: 0x5945B9, dark: 0x6A5BD0)
}

struct DurakBoardView: View {
    let board: DurakBoardState
    let perspective: Int
    let interactive: Bool
    let onMove: (String) -> Void

    var body: some View {
        GeometryReader { proxy in
            let width = min(64, max(40, proxy.size.width / 7.2))
            VStack(spacing: 12) {
                if board.hands.isEmpty {
                    Text("Waiting for opponent")
                        .font(.axFootnote)
                        .foregroundStyle(Theme.muted)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    DurakFan(hand: hand(1 - bottom), width: width)
                    HStack(alignment: .center, spacing: 8) {
                        DurakStock(board: board, width: width)
                        DurakTableView(board: board, width: width)
                        DurakDiscard(cards: Int(board.discard), width: width)
                    }
                    .frame(maxHeight: .infinity)
                    DurakHandView(
                        hand: hand(bottom),
                        legal: interactive ? board.legalMoves : [],
                        interactive: interactive,
                        width: width,
                        onMove: onMove
                    )
                    actions
                }
            }
            .padding(12)
            .frame(width: proxy.size.width, height: proxy.size.height)
            .background(DurakPalette.felt, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        }
        .aspectRatio(0.82, contentMode: .fit)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Durak table")
    }

    private var bottom: Int { perspective == 1 ? 1 : 0 }

    private func hand(_ seat: Int) -> DurakHand? {
        seat < board.hands.count ? board.hands[seat] : nil
    }

    @ViewBuilder private var actions: some View {
        let take = interactive && board.legalMoves.contains("take")
        let pass = interactive && board.legalMoves.contains("pass")
        if take || pass {
            HStack(spacing: 10) {
                if take {
                    Button("Take") { onMove("take") }
                        .buttonStyle(.bordered)
                        .accessibilityIdentifier(AccessibilityID.Durak.take)
                }
                if pass {
                    Button("Pass") { onMove("pass") }
                        .buttonStyle(.borderedProminent)
                        .accessibilityIdentifier(AccessibilityID.Durak.pass)
                }
            }
            .controlSize(.large)
        }
    }
}

struct DurakCardFace: View {
    let card: DurakCard
    let width: CGFloat

    var body: some View {
        let color = card.isRed ? DurakPalette.red : DurakPalette.ink
        RoundedRectangle(cornerRadius: width * 0.14, style: .continuous)
            .fill(DurakPalette.face)
            .overlay(RoundedRectangle(cornerRadius: width * 0.14, style: .continuous).stroke(Theme.border))
            .overlay(alignment: .topLeading) {
                VStack(spacing: 0) {
                    Text(card.rankLabel).font(.system(size: width * 0.26, weight: .semibold))
                    Text(card.symbol).font(.system(size: width * 0.22))
                }
                .padding(.leading, width * 0.1)
                .padding(.top, width * 0.06)
            }
            .overlay { Text(card.symbol).font(.system(size: width * 0.46)) }
            .foregroundStyle(color)
            .frame(width: width, height: width * 1.4)
            .shadow(color: .black.opacity(0.18), radius: 1, y: 1)
            .accessibilityLabel(card.name)
    }
}

struct DurakCardBack: View {
    let width: CGFloat

    var body: some View {
        RoundedRectangle(cornerRadius: width * 0.14, style: .continuous)
            .fill(DurakPalette.back)
            .overlay(RoundedRectangle(cornerRadius: width * 0.14, style: .continuous).stroke(DurakPalette.face, lineWidth: 2))
            .frame(width: width, height: width * 1.4)
            .shadow(color: .black.opacity(0.25), radius: 1, y: 1)
    }
}

private struct DurakFan: View {
    let hand: DurakHand?
    let width: CGFloat

    var body: some View {
        let count = Int(hand?.count ?? 0)
        let faces = hand.map { $0.hidden ? [] : $0.cards.compactMap(DurakCard.init) } ?? []
        HStack(spacing: 10) {
            HStack(spacing: -width * 0.62) {
                if faces.isEmpty {
                    ForEach(0..<min(count, 12), id: \.self) { _ in DurakCardBack(width: width * 0.78) }
                } else {
                    ForEach(faces, id: \.code) { DurakCardFace(card: $0, width: width * 0.78) }
                }
            }
            if hand?.hidden == true {
                Text("\(count)")
                    .font(.axMono(12))
                    .foregroundStyle(Theme.muted)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 2)
                    .background(Theme.card, in: Capsule())
            }
        }
        .frame(minHeight: width * 1.1)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Opponent: \(count) cards")
        .accessibilityIdentifier(AccessibilityID.Durak.opponent)
    }
}

private struct DurakStock: View {
    let board: DurakBoardState
    let width: CGFloat

    var body: some View {
        let trump = DurakCard(board.trump)
        VStack(spacing: 4) {
            ZStack(alignment: .topTrailing) {
                if board.stock > 0, let trump {
                    DurakCardFace(card: trump, width: width)
                    if board.stock > 1 { DurakCardBack(width: width).offset(x: width * 0.3) }
                } else {
                    RoundedRectangle(cornerRadius: width * 0.14, style: .continuous)
                        .strokeBorder(Theme.border, style: StrokeStyle(lineWidth: 1, dash: [4]))
                        .frame(width: width * 0.8, height: width * 1.12)
                        .overlay { Text(trump?.symbol ?? "").font(.system(size: width * 0.4)).foregroundStyle(Theme.muted) }
                }
            }
            Text("\(board.stock)").font(.axMono(12)).foregroundStyle(Theme.muted)
        }
        .frame(width: width * 1.4)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Stock: \(board.stock) cards. Trump: \(trump?.name ?? "")")
        .accessibilityIdentifier(AccessibilityID.Durak.stock)
    }
}

private struct DurakDiscard: View {
    let cards: Int
    let width: CGFloat

    var body: some View {
        VStack(spacing: 4) {
            ZStack {
                if cards != 0 {
                    DurakCardBack(width: width * 0.8).rotationEffect(.degrees(8))
                    DurakCardBack(width: width * 0.8).rotationEffect(.degrees(-6))
                } else {
                    RoundedRectangle(cornerRadius: width * 0.12, style: .continuous)
                        .strokeBorder(Theme.border, style: StrokeStyle(lineWidth: 1, dash: [4]))
                        .frame(width: width * 0.8, height: width * 1.12)
                }
            }
            Text("\(cards)").font(.axMono(12)).foregroundStyle(Theme.muted)
        }
        .frame(width: width)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Discard: \(cards) cards")
    }
}

private struct DurakTableView: View {
    let board: DurakBoardState
    let width: CGFloat

    var body: some View {
        let pairs = board.table
        let offset = width * 0.24
        ViewThatFits(in: .horizontal) {
            row(pairs, offset: offset, width: width)
            row(pairs, offset: offset * 0.8, width: width * 0.8)
            VStack(spacing: 6) {
                row(Array(pairs.prefix(3)), offset: offset * 0.8, width: width * 0.8)
                row(Array(pairs.dropFirst(3)), offset: offset * 0.8, width: width * 0.8)
            }
        }
        .frame(maxWidth: .infinity, minHeight: width * 1.7)
        .overlay {
            if pairs.isEmpty, !board.taking {
                Text("Empty table").font(.axFootnote).foregroundStyle(Theme.muted)
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Table")
        .accessibilityIdentifier(AccessibilityID.Durak.table)
    }

    private func row(_ pairs: [DurakPair], offset: CGFloat, width: CGFloat) -> some View {
        HStack(spacing: 8) {
            ForEach(pairs, id: \.attack) { pair in
                ZStack(alignment: .topLeading) {
                    if let attack = DurakCard(pair.attack) { DurakCardFace(card: attack, width: width) }
                    if pair.hasDefense, let defense = DurakCard(pair.defense) {
                        DurakCardFace(card: defense, width: width)
                            .rotationEffect(.degrees(8))
                            .offset(x: offset, y: offset)
                    }
                }
                .padding(.trailing, offset)
                .padding(.bottom, offset)
            }
        }
    }
}

private struct DurakHandView: View {
    let hand: DurakHand?
    let legal: [String]
    let interactive: Bool
    let width: CGFloat
    let onMove: (String) -> Void

    var body: some View {
        let cards = hand.map { $0.hidden ? [] : $0.cards.compactMap(DurakCard.init) } ?? []
        let overlap = cards.count > 8 ? -width * 0.5 : -width * 0.25
        HStack(spacing: overlap) {
            if cards.isEmpty {
                Text("No cards").font(.axFootnote).foregroundStyle(Theme.muted)
            }
            ForEach(cards, id: \.code) { card in
                let move = DurakNotation.move(for: card.code, legal: legal)
                Button {
                    if let move { onMove(move) }
                } label: {
                    DurakCardFace(card: card, width: width)
                        .overlay {
                            if move != nil {
                                RoundedRectangle(cornerRadius: width * 0.14, style: .continuous).stroke(Theme.primary, lineWidth: 2)
                            }
                        }
                        .offset(y: move != nil ? -6 : 0)
                        .saturation(interactive && move == nil ? 0.5 : 1)
                        .brightness(interactive && move == nil ? -0.06 : 0)
                }
                .buttonStyle(.plain)
                .disabled(move == nil)
                .accessibilityLabel("\(card.name), \(action(move))")
                .accessibilityIdentifier(AccessibilityID.Durak.card(card.code))
            }
        }
        .frame(maxWidth: .infinity, minHeight: width * 1.5)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Your hand")
    }

    private func action(_ move: String?) -> String {
        guard let move else { return "cannot be played now" }
        let parts = move.split(separator: " ")
        if parts.first == "defend", parts.count > 1, let card = DurakCard(String(parts[1])) { return "beat the \(card.name)" }
        return "attack"
    }
}
