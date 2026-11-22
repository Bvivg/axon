import AuthenticationServices
import SwiftUI

struct SessionsView: View {
    let model: ProfileViewModel
    @Environment(\.webAuthenticationSession) private var webAuthenticationSession
    @State private var ending: SignInSession?

    var body: some View {
        ProfileScroll(title: ProfilePage.sessions.title) {
            ProfileSection("Sign-in methods") {
                if let error = model.linkError {
                    ErrorBanner(text: error).padding(.bottom, 8)
                }
                ForEach(Array(OAuthProvider.allCases.enumerated()), id: \.element) { index, provider in
                    if index > 0 { Divider().overlay(Theme.line) }
                    providerRow(provider)
                }
            }
            if let ended = model.ended {
                Label(ended, systemImage: "checkmark.circle")
                    .font(.geist(13, .medium, relativeTo: .footnote))
                    .foregroundStyle(Theme.success)
            }
            if let error = model.endError {
                ErrorBanner(text: error)
            }
            if let error = model.sessionsError {
                ErrorBanner(text: error)
            }
            if !model.sessionsLoaded {
                ProgressView().frame(maxWidth: .infinity)
            } else if model.sessionsError == nil {
                ProfileSection("Current session") {
                    rows(model.current, empty: "Current session unavailable.")
                }
                ProfileSection("Other devices") {
                    rows(model.others, empty: "No other active sessions.")
                }
            }
        }
        .toolbar {
            if model.sessionsLoaded {
                ToolbarItem(placement: .primaryAction) {
                    Text(verbatim: "\(model.sessions.count) active")
                        .font(.axMono(12))
                        .foregroundStyle(Theme.muted)
                        .fixedSize()
                }
                .sharedBackgroundVisibility(.hidden)
            }
        }
        .task {
            async let account: Void = model.loadAccount()
            async let sessions: Void = model.loadSessions()
            _ = await (account, sessions)
        }
        .refreshable { await model.loadSessions() }
        .confirmationDialog(
            "End this session?",
            isPresented: Binding(get: { ending != nil }, set: { if !$0 { ending = nil } }),
            titleVisibility: .visible,
            presenting: ending
        ) { session in
            Button("End session", role: .destructive) {
                Task { await model.end(session) }
            }
        } message: { session in
            Text("\(session.device) will be signed out of Axon.")
        }
    }

    private func providerRow(_ provider: OAuthProvider) -> some View {
        HStack(spacing: 12) {
            Image(provider.logo)
                .resizable()
                .scaledToFit()
                .frame(width: 18, height: 18)
                .frame(width: 36, height: 36)
                .background(Theme.secondary, in: .rect(cornerRadius: 10, style: .continuous))
            Text(provider.title)
                .font(.geist(15, .medium, relativeTo: .body))
                .foregroundStyle(Theme.foreground)
            Spacer()
            if !model.linkedLoaded && model.linkError == nil {
                ProgressView().controlSize(.small)
            } else if model.linked.contains(provider) {
                StatusBadge(text: "Connected", tone: .success, dot: true)
            } else {
                Button {
                    Task { await model.connect(provider, present: present) }
                } label: {
                    if model.linking == provider {
                        ProgressView().controlSize(.small)
                    } else {
                        Text("Connect").font(.geist(13, .medium, relativeTo: .footnote))
                    }
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.roundedRectangle(radius: 8))
                .disabled(model.linking != nil)
                .accessibilityIdentifier(AccessibilityID.Profile.connect(provider.rawValue))
            }
        }
        .padding(.vertical, 6)
    }

    @ViewBuilder
    private func rows(_ sessions: [SignInSession], empty: String) -> some View {
        if sessions.isEmpty {
            Text(empty).font(.axSubheadline).foregroundStyle(Theme.muted)
        }
        ForEach(Array(sessions.enumerated()), id: \.element.id) { index, session in
            if index > 0 { Divider().overlay(Theme.line) }
            SessionDeviceRow(session: session, busy: model.ending == session.id) {
                ending = session
            }
        }
    }

    private func present(_ url: URL) async throws -> URL? {
        do {
            return try await webAuthenticationSession.authenticate(
                using: url,
                callback: .customScheme(OAuthCallback.scheme),
                preferredBrowserSession: .shared,
                additionalHeaderFields: [:]
            )
        } catch let error as ASWebAuthenticationSessionError where error.code == .canceledLogin {
            return nil
        }
    }
}

private struct SessionDeviceRow: View {
    let session: SignInSession
    let busy: Bool
    let onEnd: () -> Void

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            Image(systemName: session.isMobile ? "iphone" : "desktopcomputer")
                .font(.system(size: 16))
                .foregroundStyle(Theme.muted)
                .frame(width: 36, height: 36)
                .background(Theme.secondary, in: .rect(cornerRadius: 10, style: .continuous))
            VStack(alignment: .leading, spacing: 4) {
                Text(session.device)
                    .font(.geist(14, .medium, relativeTo: .subheadline))
                    .foregroundStyle(Theme.foreground)
                Text(details)
                    .font(.axMono(11, relativeTo: .caption2))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(2)
                HStack(spacing: 6) {
                    if session.current {
                        StatusBadge(text: "This device", tone: .signal, dot: true)
                    }
                    StatusBadge(text: session.online ? "Online" : "Offline", tone: session.online ? .success : .neutral, dot: true)
                }
            }
            Spacer(minLength: 8)
            if !session.current {
                Button(action: onEnd) {
                    if busy {
                        ProgressView().controlSize(.small)
                    } else {
                        Text("End").font(.geist(13, .medium, relativeTo: .footnote))
                    }
                }
                .buttonStyle(.borderless)
                .tint(Theme.destructive)
                .disabled(busy)
                .accessibilityIdentifier(AccessibilityID.Profile.endSession(session.id))
            }
        }
        .padding(.vertical, 8)
    }

    private var details: String {
        let ip = session.ip.isEmpty ? "unknown IP" : session.ip
        var text = "\(ip) · signed in \(Clock.short(session.startedAt.date))"
        if session.hasLastUsedAt {
            text += " · last used \(Clock.short(session.lastUsedAt.date))"
        }
        return text
    }
}

extension OAuthProvider {
    var logo: String {
        switch self {
        case .github: "GitHubMark"
        }
    }
}
