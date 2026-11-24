import SwiftUI

extension ToolbarItemPlacement {
    static var leadingAvatar: ToolbarItemPlacement {
        #if os(iOS)
        .topBarLeading
        #else
        .navigation
        #endif
    }
}
