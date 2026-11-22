import PhotosUI
import SwiftUI

struct AccountView: View {
    let model: ProfileViewModel
    let onSignOut: () -> Void
    @State private var confirmsSignOut = false

    var body: some View {
        let user = model.user
        ProfileScroll(title: ProfilePage.account.title) {
            HStack(spacing: 16) {
                Avatar(name: user.personName, url: user.avatarLink, size: 64, cornerRadius: 20)
                VStack(alignment: .leading, spacing: 3) {
                    Text(user.personName)
                        .font(.geist(18, .semibold, relativeTo: .title3))
                        .foregroundStyle(Theme.foreground)
                        .lineLimit(1)
                    Text(user.handle)
                        .font(.axFootnote)
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
            }
            .surface(padding: 16)

            Grid(alignment: .leading, horizontalSpacing: 20, verticalSpacing: 12) {
                GridRow {
                    Text("Email").foregroundStyle(Theme.muted)
                    HStack(spacing: 8) {
                        Text(user.email).lineLimit(1).truncationMode(.middle)
                        EmailBadge(verified: user.emailVerified)
                    }
                }
                if user.hasCreatedAt {
                    GridRow {
                        Text("Member since").foregroundStyle(Theme.muted)
                        Text(user.createdAt.date.formatted(date: .long, time: .omitted))
                    }
                }
                if !user.roles.isEmpty {
                    GridRow {
                        Text("Roles").foregroundStyle(Theme.muted)
                        HStack(spacing: 6) {
                            ForEach(user.roles, id: \.self) { role in
                                Text(role)
                                    .font(.axMono(11))
                                    .padding(.horizontal, 7)
                                    .padding(.vertical, 2)
                                    .background(Theme.secondary, in: .rect(cornerRadius: 6))
                            }
                        }
                    }
                }
            }
            .font(.axSubheadline)
            .foregroundStyle(Theme.foreground)
            .surface(padding: 16)

            Button(role: .destructive) {
                confirmsSignOut = true
            } label: {
                Label("Sign out", systemImage: "rectangle.portrait.and.arrow.right")
                    .font(.geist(15, .medium, relativeTo: .body))
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.vertical, 12)
                    .padding(.horizontal, 16)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .foregroundStyle(Theme.destructive)
            .accessibilityIdentifier(AccessibilityID.Auth.logout)
            .confirmationDialog("Sign out of Axon?", isPresented: $confirmsSignOut, titleVisibility: .visible) {
                Button("Sign out", role: .destructive, action: onSignOut)
            }
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                NavigationLink(value: Route.profile(.personal)) {
                    Label("Edit profile", systemImage: "pencil")
                }
            }
        }
    }
}

struct EmailBadge: View {
    let verified: Bool

    var body: some View {
        StatusBadge(text: verified ? "Verified" : "Unverified", tone: verified ? .success : .warning)
    }
}

struct PersonalInfoView: View {
    @Bindable var model: ProfileViewModel
    @FocusState private var focus: Field?
    @State private var photo: PhotosPickerItem?

    private enum Field: Hashable {
        case email, username, firstName, lastName, nickname
    }

    var body: some View {
        ProfileScroll(title: ProfilePage.personal.title) {
            ProfileSection("Photo") {
                VStack(alignment: .leading, spacing: 12) {
                    HStack(spacing: 16) {
                        Avatar(name: model.user.personName, url: model.user.avatarLink, size: 64, cornerRadius: 32)
                            .overlay {
                                if model.uploadingPhoto {
                                    ProgressView().frame(width: 64, height: 64).background(.ultraThinMaterial, in: Circle())
                                }
                            }
                        VStack(alignment: .leading, spacing: 6) {
                            Text(model.user.personName)
                                .font(.axHeadline)
                                .foregroundStyle(Theme.foreground)
                            let photoLabel = model.user.avatarLink == nil ? "Add photo" : "Change photo"
                            PhotosPicker(selection: $photo, matching: .images) {
                                Text(photoLabel)
                                    .font(.geist(14, .medium, relativeTo: .subheadline))
                            }
                            .disabled(model.uploadingPhoto)
                            .accessibilityIdentifier(AccessibilityID.Profile.photo)
                        }
                    }
                    if let error = model.photoError {
                        ErrorBanner(text: error)
                    }
                }
                .onChange(of: photo) { _, item in
                    guard let item else { return }
                    photo = nil
                    Task {
                        guard let data = try? await item.loadTransferable(type: Data.self) else { return }
                        await model.uploadPhoto(data)
                    }
                }
            }
            ProfileSection("Account") {
                VStack(alignment: .leading, spacing: 14) {
                    if let error = model.saveError {
                        ErrorBanner(text: error)
                    }
                    FormField("Email", hint: model.emailChanged ? "Changing your email marks it unverified again." : nil) {
                        HStack {
                            TextField("Email", text: $model.email)
                                .textContentType(.emailAddress)
                                #if os(iOS)
                                .keyboardType(.emailAddress)
                                .textInputAutocapitalization(.never)
                                #endif
                                .autocorrectionDisabled()
                                .focused($focus, equals: .email)
                                .accessibilityIdentifier(AccessibilityID.Profile.email)
                            if !model.emailChanged {
                                EmailBadge(verified: model.user.emailVerified)
                            }
                        }
                    }
                    FormField("Username", error: model.usernameError) {
                        HStack(spacing: 2) {
                            Text("@").foregroundStyle(Theme.muted)
                            TextField("username", text: $model.username)
                                .textContentType(.username)
                                #if os(iOS)
                                .textInputAutocapitalization(.never)
                                #endif
                                .autocorrectionDisabled()
                                .focused($focus, equals: .username)
                                .accessibilityIdentifier(AccessibilityID.Profile.username)
                        }
                    }
                    ViewThatFits(in: .horizontal) {
                        HStack(alignment: .top, spacing: 12) { nameFields }
                        VStack(alignment: .leading, spacing: 14) { nameFields }
                    }
                    FormField("Nickname", optional: true) {
                        TextField("Nickname", text: $model.nickname)
                            .textContentType(.nickname)
                            .focused($focus, equals: .nickname)
                            .accessibilityIdentifier(AccessibilityID.Profile.nickname)
                    }
                    HStack(spacing: 12) {
                        Button {
                            focus = nil
                            Task { await model.save() }
                        } label: {
                            HStack(spacing: 8) {
                                if model.saving { ProgressView().controlSize(.small) }
                                Text(model.saving ? "Saving…" : "Save changes")
                                    .font(.geist(15, .medium, relativeTo: .body))
                            }
                            .padding(.vertical, 4)
                        }
                        .buttonStyle(.borderedProminent)
                        .buttonBorderShape(.roundedRectangle(radius: 10))
                        .controlSize(.large)
                        .disabled(!model.canSave)
                        .accessibilityIdentifier(AccessibilityID.Profile.save)
                        if model.saved {
                            Label("Changes saved", systemImage: "checkmark")
                                .font(.geist(13, .medium, relativeTo: .footnote))
                                .foregroundStyle(Theme.success)
                                .transition(.opacity)
                                .accessibilityIdentifier(AccessibilityID.Profile.saved)
                        }
                    }
                    .padding(.top, 4)
                    .animation(.snappy, value: model.saved)
                }
            }
        }
        .onAppear { model.resetForm() }
    }

    @ViewBuilder
    private var nameFields: some View {
        FormField("First name") {
            TextField("First name", text: $model.firstName)
                .textContentType(.givenName)
                .focused($focus, equals: .firstName)
                .accessibilityIdentifier(AccessibilityID.Profile.firstName)
        }
        FormField("Last name") {
            TextField("Last name", text: $model.lastName)
                .textContentType(.familyName)
                .focused($focus, equals: .lastName)
                .accessibilityIdentifier(AccessibilityID.Profile.lastName)
        }
    }
}

struct FormField<Input: View>: View {
    let label: String
    var optional = false
    var hint: String?
    var error: String?
    @ViewBuilder let input: () -> Input

    init(_ label: String, optional: Bool = false, hint: String? = nil, error: String? = nil, @ViewBuilder input: @escaping () -> Input) {
        self.label = label
        self.optional = optional
        self.hint = hint
        self.error = error
        self.input = input
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 4) {
                Text(label).font(.geist(13, .medium, relativeTo: .footnote))
                if optional {
                    Text("· optional").font(.axFootnote).foregroundStyle(Theme.muted)
                }
            }
            .foregroundStyle(Theme.foreground)
            input()
                .font(.axBody)
                .padding(.horizontal, 12)
                .frame(minHeight: 44)
                .background(Theme.card, in: .rect(cornerRadius: 10, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .strokeBorder(error == nil ? Theme.border : Theme.destructive, lineWidth: 1)
                }
            if let error {
                Text(error).font(.axFootnote).foregroundStyle(Theme.destructive)
            } else if let hint {
                Text(hint).font(.axFootnote).foregroundStyle(Theme.muted)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
