package quoridor

const Unreachable = -1

func search(b Board, from Cell, goalRow int) (Cell, [Size * Size]Cell, int) {
	var parent [Size * Size]Cell
	if from.Row() == goalRow {
		return from, parent, 0
	}
	var dist [Size * Size]int8
	var queue [Size * Size]Cell
	dist[from] = 1
	queue[0] = from
	head, tail := 0, 1
	for head < tail {
		cur := queue[head]
		head++
		for dir := range directions {
			next, ok := b.step(cur, dir)
			if !ok || dist[next] != 0 {
				continue
			}
			dist[next] = dist[cur] + 1
			parent[next] = cur
			if next.Row() == goalRow {
				return next, parent, int(dist[next]) - 1
			}
			queue[tail] = next
			tail++
		}
	}
	return 0, parent, Unreachable
}

func Distance(b Board, from Cell, goalRow int) int {
	_, _, d := search(b, from, goalRow)
	return d
}

func HasPath(b Board, from Cell, goalRow int) bool {
	return Distance(b, from, goalRow) != Unreachable
}

func cuts(b Board, from Cell, goalRow int) ([2]uint64, bool) {
	var out [2]uint64
	end, parent, d := search(b, from, goalRow)
	if d == Unreachable {
		return out, false
	}
	for cur := end; cur != from; cur = parent[cur] {
		prev := parent[cur]
		if prev.Col() == cur.Col() {
			row, col := min(prev.Row(), cur.Row()), cur.Col()
			if col < anchors {
				out[0] |= bit(row, col)
			}
			if col > 0 {
				out[0] |= bit(row, col-1)
			}
			continue
		}
		row, col := cur.Row(), min(prev.Col(), cur.Col())
		if row < anchors {
			out[1] |= bit(row, col)
		}
		if row > 0 {
			out[1] |= bit(row-1, col)
		}
	}
	return out, true
}
