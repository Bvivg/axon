import SwiftUI

struct TextWithFooter<Footer: View>: View {
    let text: String
    @ViewBuilder let footer: () -> Footer

    var body: some View {
        if text.isEmpty {
            footer().frame(maxWidth: .infinity, alignment: .trailing)
        } else {
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .lastTextBaseline, spacing: 8) {
                    LinkedText(text: text)
                    footer().fixedSize()
                }
                VStack(alignment: .trailing, spacing: 2) {
                    LinkedText(text: text).frame(maxWidth: .infinity, alignment: .leading)
                    footer()
                }
            }
        }
    }
}
