import SwiftUI

struct TournamentsView: View {
    @State private var model: TournamentsViewModel
    @State private var composing = false
    let open: (Route) -> Void

    init(api: any TournamentsAPI, open: @escaping (Route) -> Void) {
        _model = State(initialValue: TournamentsViewModel(api: api))
        self.open = open
    }

    var body: some View {
        List {
            if let error = model.error {
                ErrorBanner(text: error)
                    .plainRow()
            }
            if model.tournaments.isEmpty {
                Group {
                    if model.loaded {
                        EmptyState(title: "No tournaments yet", systemImage: "trophy")
                            .frame(maxWidth: .infinity)
                    } else {
                        ProgressView()
                            .frame(maxWidth: .infinity)
                            .padding(32)
                    }
                }
                .plainRow()
            }
            ForEach(model.tournaments, id: \.id) { tournament in
                NavigationLink(value: Route.tournament(tournament.id)) {
                    HStack(spacing: 8) {
                        CatalogRow(title: tournament.name, subtitle: tournament.summary) {
                            GameBadge(kind: tournament.kind)
                        }
                        TournamentStatusBadge(status: tournament.status)
                    }
                }
                .accessibilityIdentifier(AccessibilityID.Tournaments.row(tournament.id))
                .plainRow()
            }
        }
        .listStyle(.plain)
        .themedList()
        .navigationTitle("Tournaments")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button("New tournament", systemImage: "plus") { composing = true }
                    .accessibilityIdentifier(AccessibilityID.Tournaments.create)
            }
        }
        .sheet(isPresented: $composing, onDismiss: model.discardDraft) {
            CreateTournamentSheet(model: model) { tournament in
                composing = false
                open(.tournament(tournament.id))
            }
        }
        .refreshable { await model.refresh() }
        .task { await model.run() }
        .accessibilityIdentifier(AccessibilityID.Tournaments.root)
    }
}

struct TournamentStatusBadge: View {
    let status: TournamentStatus

    private var tint: Color {
        switch status {
        case .running, .seeding: Theme.primary
        case .registration: Theme.success
        case .cancelled: Theme.destructive
        default: Theme.muted
        }
    }

    var body: some View {
        HStack(spacing: 5) {
            Circle().fill(tint).frame(width: 6, height: 6)
            Text(status.title)
        }
        .font(.axCaption)
        .foregroundStyle(tint)
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(tint.opacity(0.12), in: Capsule())
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier(AccessibilityID.Tournaments.status)
    }
}

struct TournamentTag: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.axCaption)
            .foregroundStyle(Theme.foreground)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(Theme.secondary, in: Capsule())
    }
}

private struct CreateTournamentSheet: View {
    let model: TournamentsViewModel
    let created: (Tournament) -> Void
    @State private var spec = NewTournament(name: "", game: .ticTacToe, format: .singleElimination, capacity: 8, rounds: 3)
    @Environment(\.dismiss) private var dismiss

    private var trimmed: String { spec.name.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Name", text: $spec.name)
                        .onChange(of: spec.name) { _, name in
                            if name.count > 64 { spec.name = String(name.prefix(64)) }
                        }
                        .accessibilityIdentifier(AccessibilityID.Tournaments.name)
                }
                Section("Game") {
                    Picker("Game", selection: $spec.game) {
                        ForEach(GameKind.tournamentGames) { Text($0.title).tag($0) }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }
                Section("Format") {
                    Picker("Format", selection: $spec.format) {
                        ForEach(TournamentFormat.offered, id: \.self) { Text($0.title).tag($0) }
                    }
                    .pickerStyle(.segmented)
                }
                Section("Players") {
                    Picker("Players", selection: $spec.capacity) {
                        ForEach([4, 8, 16], id: \.self) { Text("\($0)").tag($0) }
                    }
                    .pickerStyle(.segmented)
                }
                if spec.format == .swiss {
                    Section("Rounds") {
                        Picker("Rounds", selection: Binding(get: { spec.rounds ?? 3 }, set: { spec.rounds = $0 })) {
                            ForEach([3, 4, 5], id: \.self) { Text("\($0)").tag($0) }
                        }
                        .pickerStyle(.segmented)
                    }
                }
                if let error = model.createError {
                    ErrorBanner(text: error)
                        .listRowBackground(Color.clear)
                        .listRowInsets(EdgeInsets())
                }
            }
            .navigationTitle("New tournament")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                        .disabled(model.creating)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(model.creating ? "Creating…" : "Create") {
                        Task {
                            if let tournament = await model.create(spec) { created(tournament) }
                        }
                    }
                    .disabled(trimmed.isEmpty || model.creating)
                    .accessibilityIdentifier(AccessibilityID.Tournaments.submit)
                }
            }
            .interactiveDismissDisabled(model.creating)
        }
        #if os(macOS)
        .frame(minWidth: 420, minHeight: 480)
        #endif
    }
}
