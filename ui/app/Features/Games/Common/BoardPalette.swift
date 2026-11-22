import SwiftUI

struct BoardPalette: Sendable {
    let violet: Color
    let green: Color
    let onAccent: Color
    let danger: Color
    let chessLight: Color
    let chessDark: Color
    let chessMark: Color
    let chessLast: Color
    let chessCheck: Color
    let pieceWhite: Color
    let pieceBlack: Color
    let quoridorCell: Color
    let quoridorGroove: Color
    let quoridorWall: Color
    let quoridorWallLast: Color
    let quoridorTarget: Color
    let scrim: Color

    func seat(_ seat: Int) -> Color {
        seat == 1 ? green : violet
    }

    static func resolve(_ scheme: ColorScheme) -> BoardPalette {
        scheme == .dark ? dark : light
    }

    static let light = BoardPalette(
        violet: Color(rgb: 0x5945B9),
        green: Color(rgb: 0x237A57),
        onAccent: Color(rgb: 0xFFFFFF),
        danger: Color(rgb: 0xB8324C),
        chessLight: Color(rgb: 0xEEEAF9),
        chessDark: Color(rgb: 0xA99CE0),
        chessMark: Color(rgb: 0x34248C, opacity: 0.38),
        chessLast: Color(rgb: 0xF2BD64, opacity: 0.5),
        chessCheck: Color(rgb: 0xB8324C, opacity: 0.6),
        pieceWhite: Color(rgb: 0xFBFAFF),
        pieceBlack: Color(rgb: 0x2A2640),
        quoridorCell: Color(rgb: 0xE9E5F8),
        quoridorGroove: Color(rgb: 0xD7D1F0),
        quoridorWall: Color(rgb: 0x98600A),
        quoridorWallLast: Color(rgb: 0x5945B9),
        quoridorTarget: Color(rgb: 0x5945B9, opacity: 0.35),
        scrim: Color(rgb: 0x25283A, opacity: 0.38)
    )

    static let dark = BoardPalette(
        violet: Color(rgb: 0xAD9FFC),
        green: Color(rgb: 0x72CE9F),
        onAccent: Color(rgb: 0x0E121B),
        danger: Color(rgb: 0xF07F91),
        chessLight: Color(rgb: 0xCDC6EE),
        chessDark: Color(rgb: 0x8576CC),
        chessMark: Color(rgb: 0x34248C, opacity: 0.38),
        chessLast: Color(rgb: 0xF2BD64, opacity: 0.5),
        chessCheck: Color(rgb: 0xF07F91, opacity: 0.6),
        pieceWhite: Color(rgb: 0xFBFAFF),
        pieceBlack: Color(rgb: 0x2A2640),
        quoridorCell: Color(rgb: 0x262B3B),
        quoridorGroove: Color(rgb: 0x161A26),
        quoridorWall: Color(rgb: 0xF2BD64),
        quoridorWallLast: Color(rgb: 0xAD9FFC),
        quoridorTarget: Color(rgb: 0xAD9FFC, opacity: 0.4),
        scrim: Color(rgb: 0x000000, opacity: 0.55)
    )
}
