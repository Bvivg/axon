import SwiftUI

struct TournamentView: View {
    @State private var model: TournamentViewModel
    @State private var reason = ""

    init(id: String, services: Services, user: User) {
        _model = State(initialValue: TournamentViewModel(id: id, services: services, user: user))
    }

    var body: some View {
        Group {
            if let tournament = model.tournament {
                content(tournament)
            } else if let error = model.error {
                ContentUnavailableView {
                    Label("Could not load the tournament", systemImage: "trophy")
                } description: {
                    Text(error)
                } actions: {
                    Button("Try again") { Task { await model.refresh() } }
                        .buttonStyle(.glassProminent)
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background(Theme.background)
        .navigationTitle("Tournament")
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .task { await model.run() }
        .alert(confirmTitle, isPresented: confirming) {
            TextField("Reason", text: $reason)
            let cancelling = model.confirming == .cancel
            Button(cancelling ? "Cancel tournament" : "Start tournament", role: cancelling ? .destructive : nil) {
                guard let action = model.confirming else { return }
                let note = reason.trimmingCharacters(in: .whitespacesAndNewlines)
                Task { await model.perform(action, reason: note.isEmpty ? nil : note) }
            }
            Button("Back", role: .cancel) {}
        } message: {
            Text(model.confirming == .cancel
                ? "Games in progress are stopped and the tournament ends without a winner."
                : "Registration closes and the first round is paired with the players already registered.")
        }
    }

    private var confirming: Binding<Bool> {
        Binding(get: { model.confirming != nil }, set: { shown in
            if !shown {
                model.confirming = nil
                reason = ""
            }
        })
    }

    private var confirmTitle: String {
        let name = model.tournament?.name ?? "the tournament"
        return model.confirming == .cancel ? "Cancel \(name)?" : "Start \(name) now?"
    }

    private func content(_ tournament: Tournament) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                TournamentHeader(model: model, tournament: tournament)
                if tournament.status == .finished, tournament.hasWinnerID {
                    Label {
                        Text("Winner \(Text(model.name(tournament.winnerID)).bold())")
                    } icon: {
                        Image(systemName: "trophy.fill").foregroundStyle(Theme.primary)
                    }
                    .font(.axCallout)
                    .surface()
                    .accessibilityIdentifier(AccessibilityID.Tournaments.winner)
                }
                if !tournament.rounds.isEmpty {
                    TournamentSection(title: tournament.format == .swiss ? "Rounds" : "Bracket") {
                        if tournament.format == .swiss {
                            TournamentSwissRounds(model: model, tournament: tournament)
                        } else {
                            TournamentBracket(model: model, tournament: tournament)
                        }
                    }
                }
                if !tournament.standings.isEmpty {
                    TournamentSection(title: "Standings") {
                        TournamentStandings(model: model, tournament: tournament)
                    }
                }
                TournamentSection(title: "Players", aside: "\(tournament.playerCount)/\(tournament.capacity)") {
                    TournamentRegistrations(model: model, tournament: tournament)
                }
            }
            .padding(16)
            .frame(maxWidth: 900, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .refreshable { await model.refresh() }
    }
}

private struct TournamentHeader: View {
    let model: TournamentViewModel
    let tournament: Tournament

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 10) {
                GameBadge(kind: tournament.kind, size: 40)
                VStack(alignment: .leading, spacing: 4) {
                    Text(tournament.name)
                        .font(.axTitle3)
                        .foregroundStyle(Theme.foreground)
                    TournamentStatusBadge(status: tournament.status)
                }
            }
            ScrollView(.horizontal) {
                HStack(spacing: 6) {
                    TournamentTag(text: tournament.kind.title)
                    TournamentTag(text: tournament.format.title)
                    TournamentTag(text: "\(tournament.playerCount)/\(tournament.capacity) players")
                    if !tournament.rounds.isEmpty {
                        TournamentTag(text: "Round \(tournament.currentRound)/\(tournament.totalRounds)")
                    }
                }
            }
            .scrollIndicators(.hidden)
            TournamentActions(model: model)
        }
    }
}

private struct TournamentActions: View {
    let model: TournamentViewModel

    var body: some View {
        let any = model.canRegister || model.canWithdraw || model.canStart || model.canCancel
        if any || model.actionError != nil {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 8) {
                    if model.canRegister {
                        Button("Register") { model.request(.register) }
                            .buttonStyle(.borderedProminent)
                            .accessibilityIdentifier(AccessibilityID.Tournaments.register)
                    }
                    if model.canWithdraw {
                        Button("Withdraw") { model.request(.withdraw) }
                            .buttonStyle(.bordered)
                            .accessibilityIdentifier(AccessibilityID.Tournaments.withdraw)
                    }
                    if model.canStart {
                        Button("Start", systemImage: "play.fill") { model.request(.start) }
                            .buttonStyle(.borderedProminent)
                            .disabled(!model.startEnabled)
                            .accessibilityIdentifier(AccessibilityID.Tournaments.start)
                    }
                    if model.canCancel {
                        Button("Cancel tournament", role: .destructive) { model.request(.cancel) }
                            .buttonStyle(.bordered)
                            .tint(Theme.destructive)
                            .accessibilityIdentifier(AccessibilityID.Tournaments.cancel)
                    }
                }
                .controlSize(.regular)
                .disabled(model.busy)
                if let error = model.actionError {
                    Text(error)
                        .font(.axFootnote)
                        .foregroundStyle(Theme.destructive)
                        .accessibilityIdentifier(AccessibilityID.Tournaments.error)
                }
            }
        }
    }
}

private struct TournamentSection<Content: View>: View {
    let title: String
    var aside: String?
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Eyebrow(title)
                Spacer()
                if let aside {
                    Text(aside).font(.axCaption).foregroundStyle(Theme.muted)
                }
            }
            content()
        }
    }
}
