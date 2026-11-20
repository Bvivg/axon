package quoridor

const Unreachable = -1

func Distance(b Board, from Cell, goalRow int) int {
	if from.Row() == goalRow {
		return 0
	}
	dist := [Size * Size]int{}
	for i := range dist {
		dist[i] = Unreachable
	}
	dist[from] = 0
	queue := []Cell{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range b.neighbours(cur) {
			if dist[next] != Unreachable {
				continue
			}
			dist[next] = dist[cur] + 1
			if next.Row() == goalRow {
				return dist[next]
			}
			queue = append(queue, next)
		}
	}
	return Unreachable
}

func HasPath(b Board, from Cell, goalRow int) bool {
	return Distance(b, from, goalRow) != Unreachable
}
