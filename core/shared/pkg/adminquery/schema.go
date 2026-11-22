package adminquery

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/postgres"
)

type Kind int

const (
	String Kind = iota
	UUID
	Enum
	Int
	Time
	Bool
	Custom
)

type Condition struct {
	SQL  string
	Args []any
}

type Field struct {
	Name     string
	Column   string
	Kind     Kind
	Values   map[string]string
	Sortable bool
	NoFilter bool
	Build    func(op Op, value string) (Condition, error)
}

type Schema struct {
	Fields   []Field
	Default  string
	Tiebreak string
	byName   map[string]Field
}

type Query struct {
	Where *Condition
	Order string
}

type Error struct {
	Field   string
	Message string
}

func (e *Error) Error() string { return e.Field + " " + e.Message }

func NewSchema(defaultOrder, tiebreak string, fields ...Field) *Schema {
	s := &Schema{Fields: fields, Default: defaultOrder, Tiebreak: tiebreak, byName: map[string]Field{}}
	for _, f := range fields {
		s.byName[f.Name] = f
	}
	return s
}

func (s *Schema) Compile(filter, orderBy string) (Query, error) {
	var q Query
	tree, err := parse(filter)
	if err != nil {
		return Query{}, &Error{Field: "filter", Message: err.Error()}
	}
	if tree != nil {
		cond, err := s.compile(tree)
		if err != nil {
			return Query{}, &Error{Field: "filter", Message: err.Error()}
		}
		q.Where = &cond
	}
	order, err := s.order(orderBy)
	if err != nil {
		return Query{}, &Error{Field: "order_by", Message: err.Error()}
	}
	q.Order = order
	return q, nil
}

func (s *Schema) Sortable() []string {
	var out []string
	for _, f := range s.Fields {
		if f.Sortable {
			out = append(out, f.Name)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Schema) order(orderBy string) (string, error) {
	terms, err := parseOrder(orderBy)
	if err != nil {
		return "", err
	}
	if len(terms) == 0 {
		return s.Default, nil
	}
	parts := make([]string, 0, len(terms)+1)
	for _, t := range terms {
		f, ok := s.byName[t.field]
		if !ok || !f.Sortable {
			return "", fmt.Errorf("cannot sort by %q", t.field)
		}
		direction := "ASC"
		if t.desc {
			direction = "DESC"
		}
		parts = append(parts, f.Column+" "+direction+" NULLS LAST")
	}
	if s.Tiebreak != "" {
		parts = append(parts, s.Tiebreak)
	}
	return strings.Join(parts, ", "), nil
}

func (s *Schema) compile(n node) (Condition, error) {
	switch v := n.(type) {
	case comparison:
		return s.compare(v)
	case and:
		return s.join(v.terms, " AND ")
	case or:
		return s.join(v.terms, " OR ")
	case not:
		inner, err := s.compile(v.term)
		if err != nil {
			return Condition{}, err
		}
		return Condition{SQL: "NOT (" + inner.SQL + ")", Args: inner.Args}, nil
	}
	return Condition{}, fmt.Errorf("is not supported")
}

func (s *Schema) join(terms []node, sep string) (Condition, error) {
	parts := make([]string, 0, len(terms))
	var args []any
	for _, t := range terms {
		c, err := s.compile(t)
		if err != nil {
			return Condition{}, err
		}
		parts = append(parts, "("+c.SQL+")")
		args = append(args, c.Args...)
	}
	return Condition{SQL: strings.Join(parts, sep), Args: args}, nil
}

func (s *Schema) compare(c comparison) (Condition, error) {
	f, ok := s.byName[c.field]
	if !ok || f.NoFilter {
		return Condition{}, fmt.Errorf("cannot filter by %q", c.field)
	}
	unsupported := fmt.Errorf("does not support %q on %q", c.op, c.field)
	switch f.Kind {
	case String:
		switch c.op {
		case OpEq:
			return Condition{SQL: f.Column + " = ?", Args: []any{c.value}}, nil
		case OpNe:
			return Condition{SQL: f.Column + " IS DISTINCT FROM ?", Args: []any{c.value}}, nil
		case OpHas:
			return Condition{SQL: f.Column + ` ILIKE ? ESCAPE '\'`, Args: []any{postgres.Containing(c.value)}}, nil
		}
		return Condition{}, unsupported
	case UUID:
		id, err := uuid.Parse(c.value)
		if err != nil {
			return Condition{}, fmt.Errorf("needs a UUID for %q", c.field)
		}
		switch c.op {
		case OpEq, OpHas:
			return Condition{SQL: f.Column + " = ?", Args: []any{id}}, nil
		case OpNe:
			return Condition{SQL: f.Column + " IS DISTINCT FROM ?", Args: []any{id}}, nil
		}
		return Condition{}, unsupported
	case Enum:
		predicate, ok := f.Values[c.value]
		if !ok {
			return Condition{}, fmt.Errorf("has an unknown value %q for %q", c.value, c.field)
		}
		switch c.op {
		case OpEq, OpHas:
			return Condition{SQL: predicate}, nil
		case OpNe:
			return Condition{SQL: "NOT (" + predicate + ")"}, nil
		}
		return Condition{}, unsupported
	case Int:
		n, err := strconv.ParseInt(c.value, 10, 64)
		if err != nil {
			return Condition{}, fmt.Errorf("needs a whole number for %q", c.field)
		}
		return ordered(f.Column, c.op, n, unsupported)
	case Time:
		at, err := parseTime(c.value)
		if err != nil {
			return Condition{}, fmt.Errorf("needs an RFC 3339 time or a date for %q", c.field)
		}
		return ordered(f.Column, c.op, at, unsupported)
	case Bool:
		b, err := strconv.ParseBool(c.value)
		if err != nil {
			return Condition{}, fmt.Errorf("needs true or false for %q", c.field)
		}
		switch c.op {
		case OpEq, OpHas:
			return Condition{SQL: f.Column + " = ?", Args: []any{b}}, nil
		case OpNe:
			return Condition{SQL: f.Column + " <> ?", Args: []any{b}}, nil
		}
		return Condition{}, unsupported
	case Custom:
		if f.Build == nil {
			return Condition{}, unsupported
		}
		return f.Build(c.op, c.value)
	}
	return Condition{}, unsupported
}

func ordered(column string, op Op, value any, unsupported error) (Condition, error) {
	switch op {
	case OpEq, OpHas:
		return Condition{SQL: column + " = ?", Args: []any{value}}, nil
	case OpNe:
		return Condition{SQL: column + " IS DISTINCT FROM ?", Args: []any{value}}, nil
	case OpLt, OpLe, OpGt, OpGe:
		return Condition{SQL: column + " " + string(op) + " ?", Args: []any{value}}, nil
	}
	return Condition{}, unsupported
}

func parseTime(value string) (time.Time, error) {
	if at, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return at.UTC(), nil
	}
	at, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, err
	}
	return at.UTC(), nil
}
