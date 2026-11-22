import CoreText
import SwiftUI
#if os(iOS)
import UIKit
#else
import AppKit
#endif

enum Theme {
    static let background = Color(light: 0xF3F4F8, dark: 0x0E121B)
    static let foreground = Color(light: 0x25283A, dark: 0xF0EFF6)
    static let card = Color(light: 0xFFFFFF, dark: 0x191D29)
    static let popover = Color(light: 0xFFFFFF, dark: 0x202533)
    static let primary = Color(light: 0x5945B9, dark: 0xAD9FFC)
    static let onPrimary = Color(light: 0xFFFFFF, dark: 0x171127)
    static let secondary = Color(light: 0xECEEF5, dark: 0x242A39)
    static let muted = Color(light: 0x565F73, dark: 0xA5AEC2)
    static let accent = Color(light: 0xEBE6FC, dark: 0x2C2744)
    static let border = Color(light: 0xDADEE9, dark: 0x303646)
    static let line = Color(light: 0xE2E4ED, dark: 0x2B3040)
    static let sidebar = Color(light: 0xFBFBFE, dark: 0x10131D)
    static let surfaceRaised = Color(light: 0xF8F8FC, dark: 0x1B2130)
    static let incoming = Color(light: 0xE9ECF5, dark: 0x242A39)
    static let outgoing = Color(light: 0xE7DEFA, dark: 0x393260)
    static let success = Color(light: 0x237A57, dark: 0x72CE9F)
    static let warning = Color(light: 0x98600A, dark: 0xF2BD64)
    static let destructive = Color(light: 0xB8324C, dark: 0xF07F91)

    static let radius: CGFloat = 16

    @MainActor
    static func configureAppearance() {
        registerFonts()
        #if os(iOS)
        let appearance = UINavigationBarAppearance()
        appearance.configureWithTransparentBackground()
        if let large = UIFont(name: GeistFace.bold.rawValue, size: 32) {
            appearance.largeTitleTextAttributes = [
                .font: UIFontMetrics(forTextStyle: .largeTitle).scaledFont(for: large),
                .kern: -0.9,
                .foregroundColor: UIColor(Theme.foreground),
            ]
        }
        if let inline = UIFont(name: GeistFace.semibold.rawValue, size: 17) {
            appearance.titleTextAttributes = [
                .font: UIFontMetrics(forTextStyle: .headline).scaledFont(for: inline),
                .foregroundColor: UIColor(Theme.foreground),
            ]
            appearance.subtitleTextAttributes = [
                .font: UIFontMetrics(forTextStyle: .footnote).scaledFont(for: UIFont(name: GeistFace.regular.rawValue, size: 12) ?? inline),
                .foregroundColor: UIColor(Theme.muted),
            ]
        }
        let navigation = UINavigationBar.appearance()
        navigation.standardAppearance = appearance
        navigation.scrollEdgeAppearance = appearance
        navigation.compactAppearance = appearance
        navigation.compactScrollEdgeAppearance = appearance
        #endif
    }
}

extension Theme {
    static func registerFonts(bundle: Bundle = .main) {
        let urls = bundle.urls(forResourcesWithExtension: "ttf", subdirectory: nil) ?? []
        for url in urls {
            CTFontManagerRegisterFontsForURL(url as CFURL, .process, nil)
        }
    }
}

enum GeistFace: String {
    case regular = "Geist-Regular"
    case medium = "Geist-Medium"
    case semibold = "Geist-SemiBold"
    case bold = "Geist-Bold"
    case mono = "GeistMono-Regular"
    case monoMedium = "GeistMono-Medium"
}

extension Font {
    static func geist(_ size: CGFloat, _ face: GeistFace = .regular, relativeTo style: Font.TextStyle = .body) -> Font {
        .custom(face.rawValue, size: size, relativeTo: style)
    }

    static let axLargeTitle = geist(32, .bold, relativeTo: .largeTitle)
    static let axTitle = geist(26, .semibold, relativeTo: .title)
    static let axTitle2 = geist(22, .semibold, relativeTo: .title2)
    static let axTitle3 = geist(19, .semibold, relativeTo: .title3)
    static let axHeadline = geist(16, .semibold, relativeTo: .headline)
    static let axBody = geist(16, .regular, relativeTo: .body)
    static let axCallout = geist(15, .regular, relativeTo: .callout)
    static let axSubheadline = geist(14, .regular, relativeTo: .subheadline)
    static let axFootnote = geist(13, .regular, relativeTo: .footnote)
    static let axCaption = geist(12, .regular, relativeTo: .caption)
    static let axCaption2 = geist(11, .regular, relativeTo: .caption2)
    static let axEyebrow = geist(11, .monoMedium, relativeTo: .caption2)

    static func axMono(_ size: CGFloat, medium: Bool = false, relativeTo style: Font.TextStyle = .body) -> Font {
        geist(size, medium ? .monoMedium : .mono, relativeTo: style)
    }
}

extension Color {
    init(rgb: UInt32, opacity: Double = 1) {
        self.init(
            .sRGB,
            red: Double((rgb >> 16) & 0xFF) / 255,
            green: Double((rgb >> 8) & 0xFF) / 255,
            blue: Double(rgb & 0xFF) / 255,
            opacity: opacity
        )
    }

    init(light: UInt32, dark: UInt32) {
        #if os(iOS)
        self.init(uiColor: UIColor { traits in
            UIColor(Color(rgb: traits.userInterfaceStyle == .dark ? dark : light))
        })
        #else
        self.init(nsColor: NSColor(name: nil) { appearance in
            let isDark = appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
            return NSColor(Color(rgb: isDark ? dark : light))
        })
        #endif
    }
}

struct Eyebrow: View {
    let text: String

    init(_ text: String) {
        self.text = text
    }

    var body: some View {
        Text(text.uppercased())
            .font(.axEyebrow)
            .tracking(0.7)
            .foregroundStyle(Theme.muted)
    }
}

struct IconTile<Glyph: View>: View {
    var size: CGFloat = 36
    @ViewBuilder let glyph: () -> Glyph

    var body: some View {
        glyph()
            .foregroundStyle(Theme.primary)
            .frame(width: size, height: size)
            .background(Theme.accent, in: .rect(cornerRadius: size * 0.28, style: .continuous))
            .accessibilityHidden(true)
    }
}

extension View {
    func surface(padding: CGFloat = 16, cornerRadius: CGFloat = Theme.radius) -> some View {
        self
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.card, in: .rect(cornerRadius: cornerRadius, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                    .strokeBorder(Theme.border, lineWidth: 1)
            }
    }

    func themedList(_ background: Color = Theme.background) -> some View {
        self
            .scrollContentBackground(.hidden)
            .background(background)
            .contentMargins(.top, 0, for: .scrollContent)
            .environment(\.defaultMinListHeaderHeight, 28)
    }

    func rootTitle(_ title: String) -> some View {
        #if os(iOS)
        navigationTitle(title)
            .toolbarTitleDisplayMode(.inlineLarge)
        #else
        navigationTitle(title)
        #endif
    }

    func searchField(_ text: Binding<String>, prompt: String) -> some View {
        #if os(iOS)
        searchable(text: text, placement: .navigationBarDrawer(displayMode: .always), prompt: Text(prompt))
        #else
        searchable(text: text, prompt: Text(prompt))
        #endif
    }
}

struct EmptyState: View {
    let title: String
    let systemImage: String

    var body: some View {
        VStack(spacing: 12) {
            IconTile(size: 52) {
                Image(systemName: systemImage).font(.system(size: 22))
            }
            Text(title)
                .font(.axHeadline)
                .foregroundStyle(Theme.foreground)
                .multilineTextAlignment(.center)
        }
        .padding(24)
    }
}
