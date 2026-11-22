package adminquery

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testSchema() *Schema {
	return NewSchema("u.created_at DESC, u.id DESC", "u.id DESC",
		Field{Name: "email", Column: "u.email", Kind: String, Sortable: true},
		Field{Name: "id", Column: "u.id", Kind: UUID},
		Field{Name: "status", Kind: Enum, Values: map[string]string{"active": "u.suspended_at IS NULL", "suspended": "u.suspended_at IS NOT NULL"}},
		Field{Name: "sessions", Column: "u.sessions", Kind: Int, Sortable: true},
		Field{Name: "joined", Column: "u.created_at", Kind: Time, Sortable: true},
		Field{Name: "verified", Column: "u.email_verified", Kind: Bool},
		Field{Name: "secret", Column: "u.password_hash", Kind: String, NoFilter: true},
		Field{Name: "role", Kind: Custom, Build: func(op Op, value string) (Condition, error) {
			if op != OpEq {
				return Condition{}, errors.New("only = is supported for role")
			}
			return Condition{SQL: "EXISTS (SELECT 1 FROM roles WHERE name = ?)", Args: []any{value}}, nil
		}},
	)
}

func TestCompileFilter(t *testing.T) {
	id := uuid.MustParse("e848b9e1-316b-4399-8740-61a7de46b5a9")
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		filter string
		sql    string
		args   []any
	}{
		{name: "empty", filter: "  "},
		{name: "string equals", filter: `email = "a@b.c"`, sql: "u.email = ?", args: []any{"a@b.c"}},
		{name: "string contains escapes wildcards", filter: `email:"50%_\\"`, sql: `u.email ILIKE ? ESCAPE '\'`, args: []any{`%50\%\_\\%`}},
		{name: "uuid", filter: "id = " + id.String(), sql: "u.id = ?", args: []any{id}},
		{name: "enum", filter: "status = suspended", sql: "u.suspended_at IS NOT NULL"},
		{name: "enum not equal", filter: "status != active", sql: "NOT (u.suspended_at IS NULL)"},
		{name: "int range", filter: "sessions >= 2 AND sessions < 5", sql: "(u.sessions >= ?) AND (u.sessions < ?)", args: []any{int64(2), int64(5)}},
		{name: "date", filter: "joined >= 2026-11-01", sql: "u.created_at >= ?", args: []any{day}},
		{name: "bool", filter: "verified = false", sql: "u.email_verified = ?", args: []any{false}},
		{name: "or binds tighter than and", filter: "status = active OR status = suspended verified = true", sql: "((u.suspended_at IS NULL) OR (u.suspended_at IS NOT NULL)) AND (u.email_verified = ?)", args: []any{true}},
		{name: "parentheses and not", filter: `NOT (email:"x" OR role = "admin")`, sql: `NOT ((u.email ILIKE ? ESCAPE '\') OR (EXISTS (SELECT 1 FROM roles WHERE name = ?)))`, args: []any{"%x%", "admin"}},
		{name: "quote injection stays a value", filter: `email = "x' OR 1=1 --"`, sql: "u.email = ?", args: []any{"x' OR 1=1 --"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := testSchema().Compile(tt.filter, "")
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if tt.sql == "" {
				if q.Where != nil {
					t.Fatalf("Where = %+v, want nil", q.Where)
				}
				return
			}
			if q.Where == nil || q.Where.SQL != tt.sql {
				t.Fatalf("SQL = %+v, want %q", q.Where, tt.sql)
			}
			if !reflect.DeepEqual(q.Where.Args, tt.args) {
				t.Fatalf("Args = %#v, want %#v", q.Where.Args, tt.args)
			}
		})
	}
}

func TestCompileRejects(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		orderBy string
		field   string
		message string
	}{
		{name: "unknown field", filter: "password = x", field: "filter", message: `cannot filter by "password"`},
		{name: "hidden field", filter: `secret = "x"`, field: "filter", message: `cannot filter by "secret"`},
		{name: "column name is not a field", filter: `u.email = "x"`, field: "filter", message: `cannot filter by "u.email"`},
		{name: "bad uuid", filter: "id = nope", field: "filter", message: "needs a UUID"},
		{name: "bad enum", filter: "status = deleted", field: "filter", message: `unknown value "deleted"`},
		{name: "contains on number", filter: "sessions < x", field: "filter", message: "whole number"},
		{name: "range on enum", filter: "status > active", field: "filter", message: `does not support ">"`},
		{name: "unterminated", filter: `email = "x`, field: "filter", message: "unterminated"},
		{name: "dangling operator", filter: "email =", field: "filter", message: "expected a value"},
		{name: "unbalanced", filter: "(email = x", field: "filter", message: "expected ')'"},
		{name: "too deep", filter: "((((((email = x))))))", field: "filter", message: "nested deeper"},
		{name: "too many", filter: strings.Repeat("sessions = 1 AND ", 24) + "sessions = 1", field: "filter", message: "more than 24"},
		{name: "too long", filter: strings.Repeat("a", 1001), field: "filter", message: "longer than"},
		{name: "unsortable", orderBy: "status", field: "order_by", message: `cannot sort by "status"`},
		{name: "bad direction", orderBy: "email sideways", field: "order_by", message: "invalid direction"},
		{name: "repeated", orderBy: "email, email desc", field: "order_by", message: "repeats"},
		{name: "sql in order", orderBy: "email; drop table users", field: "order_by", message: "invalid term"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testSchema().Compile(tt.filter, tt.orderBy)
			var qe *Error
			if !errors.As(err, &qe) {
				t.Fatalf("err = %v, want *Error", err)
			}
			if qe.Field != tt.field || !strings.Contains(qe.Message, tt.message) {
				t.Fatalf("err = %s %q, want %s containing %q", qe.Field, qe.Message, tt.field, tt.message)
			}
		})
	}
}

func TestCompileOrder(t *testing.T) {
	tests := []struct {
		orderBy string
		want    string
	}{
		{orderBy: "", want: "u.created_at DESC, u.id DESC"},
		{orderBy: "email", want: "u.email ASC NULLS LAST, u.id DESC"},
		{orderBy: "sessions desc, joined", want: "u.sessions DESC NULLS LAST, u.created_at ASC NULLS LAST, u.id DESC"},
	}
	for _, tt := range tests {
		q, err := testSchema().Compile("", tt.orderBy)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tt.orderBy, err)
		}
		if q.Order != tt.want {
			t.Fatalf("Order(%q) = %q, want %q", tt.orderBy, q.Order, tt.want)
		}
	}
}

func TestWhereNumbersPlaceholders(t *testing.T) {
	var w Where
	w.Add("a = ?", 1)
	q, err := testSchema().Compile(`email:"x" AND sessions > 2`, "")
	if err != nil {
		t.Fatal(err)
	}
	w.AddQuery(q)
	paging := w.Paginate(25, 50)
	if got := w.SQL(); got != ` WHERE a = $1 AND ((u.email ILIKE $2 ESCAPE '\') AND (u.sessions > $3))` {
		t.Fatalf("SQL = %q", got)
	}
	if paging != " LIMIT $4 OFFSET $5" {
		t.Fatalf("paging = %q", paging)
	}
	if !reflect.DeepEqual(w.Args(), []any{1, "%x%", int64(2), 25, 50}) {
		t.Fatalf("args = %#v", w.Args())
	}
}

func TestPageClamps(t *testing.T) {
	tests := []struct {
		size, offset        int32
		wantLimit, wantSkip int
	}{
		{0, 0, DefaultPageSize, 0},
		{500, -3, MaxPageSize, 0},
		{50, 200_000, 50, MaxOffset},
	}
	for _, tt := range tests {
		limit, skip := Page(tt.size, tt.offset)
		if limit != tt.wantLimit || skip != tt.wantSkip {
			t.Fatalf("Page(%d, %d) = %d, %d", tt.size, tt.offset, limit, skip)
		}
	}
}

func TestPrepareNeedsOffsetForOrder(t *testing.T) {
	var w Where
	_, err := testSchema().Prepare(Request{OrderBy: "email"}, &w)
	var qe *Error
	if !errors.As(err, &qe) || qe.Field != "order_by" {
		t.Fatalf("err = %v, want order_by error", err)
	}
	offset := int32(50)
	q, err := testSchema().Prepare(Request{Filter: "sessions > 1", OrderBy: "email", Offset: &offset, PageSize: 10}, &w)
	if err != nil {
		t.Fatal(err)
	}
	if q.Order != "u.email ASC NULLS LAST, u.id DESC" || w.SQL() != " WHERE (u.sessions > $1)" {
		t.Fatalf("order = %q, where = %q", q.Order, w.SQL())
	}
	if limit, skip := (Request{Offset: &offset, PageSize: 10}).Page(); limit != 10 || skip != 50 {
		t.Fatalf("Page = %d, %d", limit, skip)
	}
}
