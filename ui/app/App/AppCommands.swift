import SwiftUI

struct AppCommands: Commands {
    let app: AppModel

    var body: some Commands {
        CommandGroup(replacing: .newItem) {
            Button("New Chat") { app.handle(.newChat) }
                .keyboardShortcut("n")
                .disabled(app.user == nil)
        }
        CommandMenu("Go") {
            Button("Search…") { app.handle(.palette) }
                .keyboardShortcut("k")
                .disabled(app.user == nil)
            Divider()
            ForEach(Array(AppTab.allCases.enumerated()), id: \.element) { index, tab in
                Button(tab.rawValue.capitalized) { app.handle(.open(tab, [])) }
                    .keyboardShortcut(KeyEquivalent(Character("\(index + 1)")))
                    .disabled(app.user == nil)
            }
            Divider()
            Button("Tournaments") { app.handle(.open(.games, [.tournaments])) }
                .keyboardShortcut("t", modifiers: [.command, .shift])
                .disabled(app.user == nil)
        }
    }
}
