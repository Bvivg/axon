import SwiftUI

struct TournamentMatchCard: View {
    let model: TournamentViewModel
    let tournament: Tournament
    let round: Int
    let match: TournamentMatch?

    var body: some View {
        let players = match?.playerIds ?? []
        let first = players.first
        let second = players.count > 1 ? players[1] : nil
        let seeds = tournament.seeds
        VStack(spacing: 0) {
            seat(first, seed: first.flatMap { seeds[$0] })
            Divider().overlay(Theme.line)
            if match?.result == .bye {
                HStack {
                    Text("Bye").foregroundStyle(Theme.muted)
                    Spacer()
                }
                .font(.axSubheadline)
                .padding(.leading, 30)
                .frame(minHeight: 32)
            } else {
                seat(second, seed: second.flatMap { seeds[$0] })
            }
            Divider().overlay(Theme.line)
            footer
        }
        .frame(width: 220)
        .background(Theme.card, in: .rect(cornerRadius: 12, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Theme.border, lineWidth: 1)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(label(first, second))
        .accessibilityIdentifier(AccessibilityID.Tournaments.match(round, Int(match?.board ?? 0)))
    }

    private func label(_ first: String?, _ second: String?) -> String {
        guard let match else { return "Waiting for players" }
        return "Board \(match.board): \(model.name(first)) vs \(second.map(model.name) ?? "bye")"
    }

    private func seat(_ id: String?, seed: Int?) -> some View {
        let won = id != nil && match?.hasWinnerID == true && match?.winnerID == id
        return HStack(spacing: 8) {
            Text(seed.map(String.init) ?? "")
                .font(.axMono(10))
                .foregroundStyle(Theme.muted)
                .frame(width: 18, alignment: .leading)
            Text(model.name(id))
                .lineLimit(1)
                .fontWeight(won ? .semibold : .regular)
            Spacer(minLength: 4)
            if won {
                Image(systemName: "crown.fill")
                    .font(.caption)
                    .accessibilityLabel("Winner")
            }
        }
        .font(.axSubheadline)
        .foregroundStyle(won ? Theme.primary : Theme.foreground)
        .padding(.horizontal, 10)
        .frame(minHeight: 32)
    }

    @ViewBuilder private var footer: some View {
        let pending = match?.result == .pending
        let ready = match.map(model.isReady) ?? false
        let text = match == nil ? "Waiting" : pending && !ready ? "Starting…" : match?.resultLabel ?? ""
        HStack {
            Text(text).foregroundStyle(pending ? Theme.primary : Theme.muted)
            Spacer()
            if let match, ready, match.result != .aborted {
                NavigationLink(value: Route.game(match.sessionID)) {
                    Label(pending ? "Open game" : "View game", systemImage: "eye")
                        .foregroundStyle(Theme.foreground)
                }
                .buttonStyle(.plain)
            }
        }
        .font(.axCaption)
        .padding(.horizontal, 10)
        .frame(minHeight: 32)
        .background(Theme.secondary.opacity(0.5))
    }
}

struct TournamentBracket: View {
    let model: TournamentViewModel
    let tournament: Tournament

    var body: some View {
        let total = Int(tournament.totalRounds)
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 16) {
                ForEach(1...max(total, 1), id: \.self) { number in
                    let round = tournament.rounds.first { Int($0.number) == number }
                    let matches = (round?.matches ?? []).sorted { $0.board < $1.board }
                    let size = 1 << max(total - number, 0)
                    VStack(alignment: .leading, spacing: 10) {
                        Eyebrow(tournament.roundTitle(number))
                        VStack(spacing: 12) {
                            ForEach(0..<size, id: \.self) { index in
                                TournamentMatchCard(
                                    model: model,
                                    tournament: tournament,
                                    round: number,
                                    match: index < matches.count ? matches[index] : nil
                                )
                            }
                        }
                        .frame(maxHeight: .infinity)
                    }
                    .frame(maxHeight: .infinity, alignment: .top)
                }
            }
            .padding(.bottom, 8)
        }
        .scrollIndicators(.hidden)
    }
}

struct TournamentSwissRounds: View {
    let model: TournamentViewModel
    let tournament: Tournament

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            ForEach(tournament.rounds, id: \.number) { round in
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 8) {
                        Eyebrow("Round \(round.number)")
                        if round.status == .running {
                            TournamentStatusBadge(status: .running)
                        }
                    }
                    ScrollView(.horizontal) {
                        HStack(spacing: 12) {
                            ForEach(round.matches.sorted { $0.board < $1.board }, id: \.board) { match in
                                TournamentMatchCard(model: model, tournament: tournament, round: Int(round.number), match: match)
                            }
                        }
                    }
                    .scrollIndicators(.hidden)
                }
            }
        }
    }
}

struct TournamentStandings: View {
    let model: TournamentViewModel
    let tournament: Tournament

    var body: some View {
        let swiss = tournament.format == .swiss
        Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 8) {
            GridRow {
                Text("#")
                Text("Player").gridColumnAlignment(.leading)
                if swiss { Text("Pts").gridColumnAlignment(.trailing) }
                Text("W").gridColumnAlignment(.trailing)
                Text("D").gridColumnAlignment(.trailing)
                Text("L").gridColumnAlignment(.trailing)
                if !swiss { Text("Status").gridColumnAlignment(.trailing) }
            }
            .font(.axCaption)
            .foregroundStyle(Theme.muted)
            ForEach(tournament.standings, id: \.userID) { row in
                Divider().overlay(Theme.line)
                GridRow {
                    Text("\(row.rank)").font(.axMono(12))
                    Text(model.name(row.userID)).lineLimit(1).frame(maxWidth: .infinity, alignment: .leading)
                    if swiss { Text(TournamentPoints.label(row.halfPoints)).font(.axMono(13)) }
                    Text("\(row.wins)").font(.axMono(13))
                    Text("\(row.draws)").font(.axMono(13))
                    Text("\(row.losses)").font(.axMono(13))
                    if !swiss {
                        Text(status(row)).font(.axCaption).foregroundStyle(Theme.muted)
                    }
                }
                .font(.axSubheadline)
            }
        }
        .surface(padding: 12, cornerRadius: 12)
    }

    private func status(_ row: TournamentStanding) -> String {
        if tournament.hasWinnerID, tournament.winnerID == row.userID { return "Champion" }
        return row.eliminated ? "Out" : "Playing"
    }
}

struct TournamentRegistrations: View {
    let model: TournamentViewModel
    let tournament: Tournament

    var body: some View {
        let listed = tournament.registrations.sorted {
            ($0.hasSeed ? Int($0.seed) : Int.max) < ($1.hasSeed ? Int($1.seed) : Int.max)
        }
        if listed.isEmpty {
            Text("Nobody yet")
                .font(.axFootnote)
                .foregroundStyle(Theme.muted)
        } else {
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 240), spacing: 8)], spacing: 6) {
                ForEach(listed, id: \.userID) { registration in
                    HStack(spacing: 8) {
                        Text(registration.hasSeed ? "\(registration.seed)" : "")
                            .font(.axMono(10))
                            .foregroundStyle(Theme.muted)
                            .frame(width: 18, alignment: .leading)
                        Text(model.name(registration.userID))
                            .lineLimit(1)
                            .strikethrough(registration.returned)
                            .foregroundStyle(registration.returned ? Theme.muted : Theme.foreground)
                        Spacer(minLength: 4)
                        Text(String(registration.rating))
                            .font(.axMono(12))
                            .foregroundStyle(Theme.muted)
                    }
                    .font(.axSubheadline)
                    .padding(.horizontal, 10)
                    .frame(minHeight: 36)
                    .background(Theme.card, in: .rect(cornerRadius: 10, style: .continuous))
                    .overlay {
                        RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.border, lineWidth: 1)
                    }
                }
            }
            .accessibilityElement(children: .contain)
            .accessibilityLabel("Registered players")
            .accessibilityIdentifier(AccessibilityID.Tournaments.players)
        }
    }
}
