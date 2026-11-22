package store

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReportSchemaCompilesEveryField(t *testing.T) {
	id := uuid.MustParse("9a4c6f0e-31d5-4f6b-9a55-0b8b7f1f3c11")
	day := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		field  string
		filter string
		sql    string
		args   []any
	}{
		{"session", "session = " + id.String(), "session_id = ?", []any{id}},
		{"status", "status = pending", "status = 'pending'", nil},
		{"status", "status = ready", "status = 'ready'", nil},
		{"status", "status != failed", "NOT (status = 'failed')", nil},
		{"started", "started >= 2026-11-20", "started_at >= ?", []any{day}},
		{"finished", "finished < 2026-11-20", "completed_at < ?", []any{day}},
	}
	covered := map[string]bool{}
	for _, tt := range cases {
		q, err := reportSchema.Compile(tt.filter, "")
		if err != nil {
			t.Errorf("Compile(%q): %v", tt.filter, err)
			continue
		}
		if q.Where == nil || q.Where.SQL != tt.sql || !reflect.DeepEqual(q.Where.Args, tt.args) {
			t.Errorf("Compile(%q) = %+v, want %q %#v", tt.filter, q.Where, tt.sql, tt.args)
		}
		covered[tt.field] = true
	}
	for _, f := range reportSchema.Fields {
		if !covered[f.Name] {
			t.Errorf("field %q has no filter case", f.Name)
		}
		if !f.Sortable {
			continue
		}
		q, err := reportSchema.Compile("", f.Name+" desc")
		if err != nil {
			t.Errorf("order_by %q: %v", f.Name, err)
			continue
		}
		if !strings.HasPrefix(q.Order, f.Column+" DESC NULLS LAST, ") {
			t.Errorf("order_by %q = %q", f.Name, q.Order)
		}
	}
	if _, err := reportSchema.Compile("status = done", ""); err == nil {
		t.Error("an unknown status compiled")
	}
}
