package adminquery

import (
	"strconv"
	"strings"
)

const (
	DefaultPageSize = 25
	MaxPageSize     = 100
	MaxOffset       = 100_000
)

type Where struct {
	clauses []string
	args    []any
}

func (w *Where) Add(clause string, args ...any) {
	for _, arg := range args {
		w.args = append(w.args, arg)
		clause = strings.Replace(clause, "?", "$"+strconv.Itoa(len(w.args)), 1)
	}
	w.clauses = append(w.clauses, clause)
}

func (w *Where) AddQuery(q Query) {
	if q.Where != nil {
		w.Add("("+q.Where.SQL+")", q.Where.Args...)
	}
}

func (w *Where) Arg(value any) string {
	w.args = append(w.args, value)
	return "$" + strconv.Itoa(len(w.args))
}

func (w *Where) SQL() string {
	if len(w.clauses) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.clauses, " AND ")
}

func (w *Where) Args() []any {
	return w.args
}

func (w Where) Clone() Where {
	return Where{clauses: append([]string(nil), w.clauses...), args: append([]any(nil), w.args...)}
}

func Page(size, offset int32) (int, int) {
	limit := int(size)
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	skip := int(offset)
	if skip < 0 {
		skip = 0
	}
	if skip > MaxOffset {
		skip = MaxOffset
	}
	return limit, skip
}

func (w *Where) Paginate(limit, offset int) string {
	return " LIMIT " + w.Arg(limit) + " OFFSET " + w.Arg(offset)
}
