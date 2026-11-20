import SwiftUI

struct AuthView: View {
    @State private var model: AuthViewModel
    let onSignedIn: (User) -> Void
    let onSettings: () -> Void

    init(services: Services, onSignedIn: @escaping (User) -> Void, onSettings: @escaping () -> Void) {
        _model = State(initialValue: AuthViewModel(auth: services.auth))
        self.onSignedIn = onSignedIn
        self.onSettings = onSettings
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Mode", selection: $model.mode) {
                        ForEach(AuthViewModel.Mode.allCases) { mode in
                            Text(mode.rawValue).tag(mode)
                        }
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .accessibilityIdentifier(AccessibilityID.Auth.mode)
                }
                Section {
                    TextField("Email", text: $model.email)
                        .textContentType(.username)
                        .autocorrectionDisabled()
                        #if os(iOS)
                        .keyboardType(.emailAddress)
                        .textInputAutocapitalization(.never)
                        #endif
                        .accessibilityIdentifier(AccessibilityID.Auth.email)
                    if model.mode == .register {
                        TextField("Username", text: $model.username)
                            .autocorrectionDisabled()
                            #if os(iOS)
                            .textInputAutocapitalization(.never)
                            #endif
                            .accessibilityIdentifier(AccessibilityID.Auth.username)
                    }
                    SecureField("Password", text: $model.password)
                        .textContentType(.password)
                        .accessibilityIdentifier(AccessibilityID.Auth.password)
                        .onSubmit(submit)
                } footer: {
                    if let hint = model.validationMessage, !hint.isEmpty {
                        Text(hint)
                    }
                }
                Section {
                    Button(action: submit) {
                        HStack {
                            Spacer()
                            if model.isSubmitting {
                                ProgressView()
                            } else {
                                Text(model.mode.rawValue).bold()
                            }
                            Spacer()
                        }
                    }
                    .disabled(!model.canSubmit)
                    .accessibilityIdentifier(AccessibilityID.Auth.submit)
                    if let error = model.error {
                        Text(error)
                            .foregroundStyle(.red)
                            .accessibilityIdentifier(AccessibilityID.Auth.error)
                    }
                }
            }
            .formStyle(.grouped)
            .frame(maxWidth: 520)
            .frame(maxWidth: .infinity)
            .navigationTitle("Axon")
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button("Settings", systemImage: "gearshape", action: onSettings)
                        .accessibilityIdentifier(AccessibilityID.Settings.open)
                }
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
}
