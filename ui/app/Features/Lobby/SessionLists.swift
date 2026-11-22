import SwiftUI

struct SessionRow: View {
    let session: Session
    let title: String
    let me: String
    let action: () -> Void

    private var subtitle: String {
        guard session.status != .waiting else { return session.kind.title }
        return "\(session.kind.title) · \(SessionStatusText.headline(session, me: me))"
    }

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                GameBadge(kind: session.kind, size: 36)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.geist(15, .semibold, relativeTo: .body))
                        .foregroundStyle(Theme.foreground)
                        .lineLimit(1)
                    Text(subtitle)
                        .font(.axFootnote)
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: 8)
                if session.hasSpectators, session.spectators > 0 {
                    Label("\(session.spectators)", systemImage: "eye")
                        .font(.axCaption)
                        .foregroundStyle(Theme.muted)
                }
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
            }
            .padding(.vertical, 8)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier(AccessibilityID.Lobby.session(session.id))
    }
}
