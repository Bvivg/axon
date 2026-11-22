import AppIntents

enum IntentGame: String, AppEnum {
    case tictactoe
    case chess
    case quoridor
    case checkers
    case durak

    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Game"
    static let caseDisplayRepresentations: [IntentGame: DisplayRepresentation] = [
        .tictactoe: "Tic-Tac-Toe",
        .chess: "Chess",
        .quoridor: "Quoridor",
        .checkers: "Checkers",
        .durak: "Durak",
    ]

    var kind: GameKind { GameKind(game: rawValue) }
}

struct QuickMatchIntent: AppIntent {
    static let title: LocalizedStringResource = "Quick Match"
    static let description = IntentDescription("Find a rated opponent for a game in Axon.")
    static let openAppWhenRun = true

    @Parameter(title: "Game", default: .chess)
    var game: IntentGame

    @Dependency private var app: AppModel

    @MainActor
    func perform() async throws -> some IntentResult {
        app.handle(.quickMatch(game.kind))
        return .result()
    }
}

struct PracticeIntent: AppIntent {
    static let title: LocalizedStringResource = "Practice Offline"
    static let description = IntentDescription("Play a game against the computer in Axon.")
    static let openAppWhenRun = true

    @Parameter(title: "Game", default: .chess)
    var game: IntentGame

    @Dependency private var app: AppModel

    @MainActor
    func perform() async throws -> some IntentResult {
        app.handle(.open(.games, [.lobby(game.kind), .practice(game.kind)]))
        return .result()
    }
}

struct TournamentsIntent: AppIntent {
    static let title: LocalizedStringResource = "Show Tournaments"
    static let description = IntentDescription("Open the tournament list in Axon.")
    static let openAppWhenRun = true

    @Dependency private var app: AppModel

    @MainActor
    func perform() async throws -> some IntentResult {
        app.handle(.open(.games, [.tournaments]))
        return .result()
    }
}

struct AxonShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: QuickMatchIntent(),
            phrases: ["Start a quick match in \(.applicationName)", "Quick \(\.$game) match in \(.applicationName)"],
            shortTitle: "Quick Match",
            systemImageName: "bolt"
        )
        AppShortcut(
            intent: PracticeIntent(),
            phrases: ["Practice in \(.applicationName)", "Practice \(\.$game) in \(.applicationName)"],
            shortTitle: "Practice",
            systemImageName: "cpu"
        )
        AppShortcut(
            intent: TournamentsIntent(),
            phrases: ["Show tournaments in \(.applicationName)"],
            shortTitle: "Tournaments",
            systemImageName: "trophy"
        )
    }
}
