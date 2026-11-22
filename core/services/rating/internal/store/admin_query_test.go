package store

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"
)

type schemaCase struct {
	field  string
	filter string
	sql    string
	args   []any
}

func checkSchema(t *testing.T, schema *adminquery.Schema, cases []schemaCase) {
	t.Helper()
	covered := map[string]bool{}
	for _, tt := range cases {
		q, err := schema.Compile(tt.filter, "")
		if err != nil {
			t.Errorf("Compile(%q): %v", tt.filter, err)
			continue
		}
		if q.Where == nil || q.Where.SQL != tt.sql || !reflect.DeepEqual(q.Where.Args, tt.args) {
			t.Errorf("Compile(%q) = %+v, want %q %#v", tt.filter, q.Where, tt.sql, tt.args)
		}
		covered[tt.field] = true
	}
	for _, f := range schema.Fields {
		if !covered[f.Name] {
			t.Errorf("field %q has no filter case", f.Name)
		}
		if !f.Sortable {
			continue
		}
		q, err := schema.Compile("", f.Name+" desc")
		if err != nil {
			t.Errorf("order_by %q: %v", f.Name, err)
			continue
		}
		if !strings.HasPrefix(q.Order, f.Column+" DESC NULLS LAST, ") {
			t.Errorf("order_by %q = %q", f.Name, q.Order)
		}
	}
}

func TestChangeSchemaCompilesEveryField(t *testing.T) {
	id := uuid.MustParse("9a4c6f0e-31d5-4f6b-9a55-0b8b7f1f3c11")
	day := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	checkSchema(t, changeSchema, []schemaCase{
		{"user", "user = " + id.String(), "user_id = ?", []any{id}},
		{"game", `game = "chess"`, "game = ?", []any{"chess"}},
		{"session", "session = " + id.String(), "session_id = ?", []any{id}},
		{"delta", "delta < 0", "round(rating_after - rating_before) < ?", []any{int64(0)}},
		{"rating_before", "rating_before >= 1500", "round(rating_before) >= ?", []any{int64(1500)}},
		{"rating_after", "rating_after <= 1700", "round(rating_after) <= ?", []any{int64(1700)}},
		{"finished", "finished > 2026-11-20", "finished_at > ?", []any{day}},
	})
}

func TestRatingSchemaCompilesEveryField(t *testing.T) {
	id := uuid.MustParse("9a4c6f0e-31d5-4f6b-9a55-0b8b7f1f3c11")
	day := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	checkSchema(t, ratingSchema, []schemaCase{
		{"user", "user != " + id.String(), "user_id IS DISTINCT FROM ?", []any{id}},
		{"game", `game:"check"`, `game ILIKE ? ESCAPE '\'`, []any{"%check%"}},
		{"rating", "rating = 1500", "round(rating) = ?", []any{int64(1500)}},
		{"games", "games > 10", "games > ?", []any{int64(10)}},
		{"updated", "updated >= 2026-11-20", "updated_at >= ?", []any{day}},
	})
}
