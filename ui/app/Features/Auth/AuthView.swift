import AuthenticationServices
import SwiftUI

struct AuthView: View {
    @State private var model: AuthViewModel
    @Environment(\.webAuthenticationSession) private var webAuthenticationSession
    @FocusState private var focus: Field?
    let onSignedIn: (User) -> Void
    let onSettings: () -> Void

    private enum Field: Hashable {
        case email
        case username
        case password
    }

    init(services: Services, onSignedIn: @escaping (User) -> Void, onSettings: @escaping () -> Void) {
        _model = State(initialValue: AuthViewModel(auth: services.auth))
        self.onSignedIn = onSignedIn
        self.onSettings = onSettings
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 12) {
                    Picker("Mode", selection: $model.mode) {
                        ForEach(AuthViewModel.Mode.allCases) { mode in
                            Text(mode.rawValue).tag(mode)
                        }
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .accessibilityIdentifier(AccessibilityID.Auth.mode)
                    card
                }
                .padding(.horizontal, 16)
                .padding(.top, 8)
                .padding(.bottom, 20)
                .frame(maxWidth: 432)
                .frame(maxWidth: .infinity)
            }
            .scrollDismissesKeyboard(.interactively)
            .background(Theme.background)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    HStack(spacing: 10) {
                        AxonAppIcon(size: 30)
                        Text(verbatim: "AXON")
                            .font(.geist(14, .bold, relativeTo: .headline))
                            .tracking(1.4)
                            .foregroundStyle(Theme.foreground)
                    }
                    .accessibilityElement(children: .combine)
                    .accessibilityLabel("Axon")
                }
                .sharedBackgroundVisibility(.hidden)
                ToolbarItem(placement: .primaryAction) {
                    Button("Settings", systemImage: "gearshape", action: onSettings)
                        .accessibilityIdentifier(AccessibilityID.Settings.open)
                }
            }
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
        }
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(model.mode == .login ? "Sign in to Axon" : "Create your account")
                .font(.geist(20, .semibold, relativeTo: .title3))
                .tracking(-0.3)
                .foregroundStyle(Theme.foreground)
            if let error = model.error {
                ErrorBanner(text: error)
                    .accessibilityIdentifier(AccessibilityID.Auth.error)
            }
            fields
            if let hint = model.validationMessage, !hint.isEmpty {
                Text(hint)
                    .font(.axFootnote)
                    .foregroundStyle(Theme.muted)
            }
            Button(action: submit) {
                ZStack {
                    Text(model.mode == .login ? "Sign in" : "Create account").opacity(model.isSubmitting ? 0 : 1)
                    if model.isSubmitting {
                        ProgressView()
                            .controlSize(.small)
                            .frame(width: 18, height: 18)
                            .accessibilityHidden(true)
                    }
                }
                .font(.geist(15, .medium, relativeTo: .body))
                .frame(maxWidth: .infinity, minHeight: 34)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.roundedRectangle(radius: 10))
            .disabled(!model.canSubmit)
            .accessibilityIdentifier(AccessibilityID.Auth.submit)
            if !OAuthProvider.allCases.isEmpty {
                divider
                ForEach(OAuthProvider.allCases) { provider in
                    ProviderButton(provider: provider, busy: model.signingInWith == provider) {
                        signIn(with: provider)
                    }
                    .disabled(model.isBusy)
                }
            }
        }
        .surface(padding: 16, cornerRadius: 18)
        .animation(.snappy, value: model.mode)
        .animation(.snappy, value: model.error)
    }

    private var divider: some View {
        HStack(spacing: 12) {
            Rectangle().fill(Theme.line).frame(height: 1)
            Text("or")
                .font(.axFootnote)
                .foregroundStyle(Theme.muted)
            Rectangle().fill(Theme.line).frame(height: 1)
        }
    }

    private var fields: some View {
        VStack(alignment: .leading, spacing: 10) {
            FormField("Email") {
                TextField("you@example.com", text: $model.email)
                    .textContentType(.username)
                    .autocorrectionDisabled()
                    #if os(iOS)
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    #endif
                    .focused($focus, equals: .email)
                    .submitLabel(.next)
                    .onSubmit { focus = model.mode == .register ? .username : .password }
                    .accessibilityIdentifier(AccessibilityID.Auth.email)
            }
            if model.mode == .register {
                FormField("Username") {
                    TextField("username", text: $model.username)
                        .autocorrectionDisabled()
                        #if os(iOS)
                        .textInputAutocapitalization(.never)
                        #endif
                        .focused($focus, equals: .username)
                        .submitLabel(.next)
                        .onSubmit { focus = .password }
                        .accessibilityIdentifier(AccessibilityID.Auth.username)
                }
                .transition(.move(edge: .top).combined(with: .opacity))
            }
            FormField("Password") {
                SecureField("Password", text: $model.password)
                    .textContentType(.password)
                    .focused($focus, equals: .password)
                    .submitLabel(.go)
                    .onSubmit(submit)
                    .accessibilityIdentifier(AccessibilityID.Auth.password)
            }
        }
    }

    private func submit() {
        Task {
            if let user = await model.submit() {
                onSignedIn(user)
            }
        }
    }

    private func signIn(with provider: OAuthProvider) {
        focus = nil
        Task {
            let user = await model.signIn(with: provider) { url in
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
            if let user {
                onSignedIn(user)
            }
        }
    }
}

private struct ProviderButton: View {
    let provider: OAuthProvider
    let busy: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                if busy {
                    ProgressView()
                        .controlSize(.small)
                        .frame(width: 18, height: 18)
                        .accessibilityHidden(true)
                } else {
                    Image(provider.logo)
                        .resizable()
                        .scaledToFit()
                        .frame(width: 18, height: 18)
                }
                Text("Continue with \(provider.title)")
                    .font(.geist(15, .medium, relativeTo: .body))
                    .foregroundStyle(Theme.foreground)
            }
            .frame(maxWidth: .infinity)
            .frame(minHeight: 44)
            .background(Theme.card, in: .rect(cornerRadius: 10, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .strokeBorder(Theme.border, lineWidth: 1)
            }
            .contentShape(.rect(cornerRadius: 10))
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier(AccessibilityID.Auth.provider(provider.rawValue))
    }
}
