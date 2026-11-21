import CoreGraphics

struct QuoridorBoardLayout: Sendable, Equatable {
    let side: CGFloat

    var padding: CGFloat { side * 0.016 }
    var groove: CGFloat { (side - padding * 2) * 0.019 }
    var cell: CGFloat { (side - padding * 2 - groove * CGFloat(Quoridor.anchors)) / CGFloat(Quoridor.size) }
    var pitch: CGFloat { cell + groove }
    var slotReach: CGFloat { groove / 2 + cell * 0.18 }

    func cellRect(displayRow: Int, displayCol: Int) -> CGRect {
        CGRect(
            x: padding + CGFloat(displayCol) * pitch,
            y: padding + CGFloat(displayRow) * pitch,
            width: cell,
            height: cell
        )
    }

    func wallRect(_ wall: QuoridorDisplayWall) -> CGRect {
        let along = padding + CGFloat(wall.segment) * pitch
        let across = padding + CGFloat(wall.groove) * pitch + cell
        let length = cell * 2 + groove
        switch wall.orientation {
        case .horizontal: return CGRect(x: along, y: across, width: length, height: groove)
        case .vertical: return CGRect(x: across, y: along, width: groove, height: length)
        }
    }

    func slotRect(orientation: QuoridorWallOrientation, groove index: Int, segment: Int) -> CGRect {
        let along = padding + CGFloat(segment) * pitch
        let center = padding + CGFloat(index) * pitch + cell + groove / 2
        switch orientation {
        case .horizontal: return CGRect(x: along, y: center - slotReach, width: cell, height: slotReach * 2)
        case .vertical: return CGRect(x: center - slotReach, y: along, width: slotReach * 2, height: cell)
        }
    }
}
