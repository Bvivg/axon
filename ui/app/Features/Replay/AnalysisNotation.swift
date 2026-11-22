import Foundation

enum AnalysisNotation {
    static func move(_ uci: String, in fen: String) -> String? {
        if let san = ChessPractice.san(of: uci, in: fen) { return san }
        guard uci.count >= 4 else { return nil }
        let promotion = uci.count > 4 ? "=\(uci.suffix(1).uppercased())" : ""
        return "\(uci.prefix(2))–\(uci.dropFirst(2).prefix(2))\(promotion)"
    }
}
