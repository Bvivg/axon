import SwiftUI

struct SettingsView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var model: SettingsViewModel

    init(current: GatewayAddress, apply: @escaping @MainActor (GatewayAddress) async -> Void) {
        _model = State(initialValue: SettingsViewModel(current: current, apply: apply))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Gateway") {
                    TextField("http://localhost:18080", text: $model.address)
                        .autocorrectionDisabled()
                        #if os(iOS)
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        #endif
                        .accessibilityIdentifier(AccessibilityID.Settings.gatewayURL)
                        .onSubmit(save)
                    if let error = model.error {
                        Text(error)
                            .foregroundStyle(.red)
                            .accessibilityIdentifier(AccessibilityID.Settings.error)
                    } else if model.changesAccount {
                        Text("Changing the gateway signs you out.")
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .formStyle(.grouped)
            .navigationTitle("Settings")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save", action: save)
                        .disabled(model.isSaving)
                        .accessibilityIdentifier(AccessibilityID.Settings.save)
                }
            }
        }
        #if os(macOS)
        .frame(minWidth: 420, minHeight: 220)
        #endif
    }

    private func save() {
        Task {
            if await model.save() {
                dismiss()
            }
        }
    }
}
