package repository

import (
	"errors"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"
)

const someID = "e848b9e1-316b-4399-8740-61a7de46b5a9"

type fieldCase struct {
	filter string
	where  string
	order  string
}

func TestAdminSchemasCompileEveryField(t *testing.T) {
	schemas := map[string]struct {
		schema *adminquery.Schema
		fields map[string]fieldCase
	}{
		"rooms": {schema: roomSchema, fields: map[string]fieldCase{
			"id":            {filter: "id = " + someID, where: "r.id = ?"},
			"title":         {filter: `title:"lobby"`, where: `r.name ILIKE ? ESCAPE '\'`, order: "r.name"},
			"kind":          {filter: "kind = direct", where: "r.kind = 'direct'"},
			"members":       {filter: "members >= 2", where: roomMemberCount + " >= ?", order: roomMemberCount},
			"messages":      {filter: "messages > 10", where: "(r.next_seq - 1) > ?", order: "(r.next_seq - 1)"},
			"created":       {filter: "created >= 2026-11-01", where: "r.created_at >= ?", order: "r.created_at"},
			"last_activity": {filter: `last_activity < "2026-11-01T12:00:00Z"`, where: roomLastActivity + " < ?", order: roomLastActivity},
			"created_by":    {filter: "created_by = " + someID, where: "r.created_by = ?"},
			"member":        {filter: "member = " + someID, where: "EXISTS (SELECT 1 FROM room_members fm WHERE fm.room_id = r.id AND fm.user_id = ?)"},
		}},
		"uploads": {schema: uploadSchema, fields: map[string]fieldCase{
			"id":       {filter: "id = " + someID, where: "u.id = ?"},
			"kind":     {filter: "kind = image", where: "u.kind = 'image'"},
			"name":     {filter: `name:"report"`, where: uploadName + ` ILIKE ? ESCAPE '\'`, order: uploadName},
			"size":     {filter: "size >= 1024", where: uploadSize + " >= ?", order: uploadSize},
			"uploaded": {filter: "uploaded <= 2026-11-01", where: "u.created_at <= ?", order: "u.created_at"},
			"uploader": {filter: "uploader = " + someID, where: "u.uploader_id = ?"},
			"room":     {filter: "room != " + someID, where: "NOT EXISTS (SELECT 1 FROM " + uploadLinks + " WHERE linked.room_id = ?)"},
		}},
		"calls": {schema: callSchema, fields: map[string]fieldCase{
			"id":           {filter: "id = " + someID, where: "c.id = ?"},
			"state":        {filter: "state = ringing", where: "c.state = 'ringing'"},
			"active":       {filter: "active = true", where: "(c.state <> 'ended') = ?"},
			"kind":         {filter: "kind = direct", where: "c.document->>'kind' IS DISTINCT FROM 'group'"},
			"mode":         {filter: "mode = video", where: "c.document->>'mode' = 'video'"},
			"started":      {filter: "started >= 2026-11-01", where: "c.created_at >= ?", order: "c.created_at"},
			"ended":        {filter: "ended < 2026-11-02", where: callEnded + " < ?", order: callEnded},
			"duration":     {filter: "duration >= 60", where: callDuration + " >= ?", order: callDuration},
			"participants": {filter: "participants > 2", where: callParticipants + " > ?", order: callParticipants},
			"room":         {filter: "room = " + someID, where: "c.room_id = ?"},
			"user":         {filter: "user = " + someID, where: "(c.caller_id = ? OR EXISTS (SELECT 1 FROM call_participants fp WHERE fp.call_id = c.id AND fp.user_id = ?))"},
		}},
	}
	for name, s := range schemas {
		for _, field := range s.schema.Fields {
			tt, ok := s.fields[field.Name]
			if !ok {
				t.Errorf("%s.%s has no case", name, field.Name)
				continue
			}
			t.Run(name+"/"+field.Name, func(t *testing.T) {
				q, err := s.schema.Compile(tt.filter, "")
				if err != nil {
					t.Fatalf("Compile(%q): %v", tt.filter, err)
				}
				if q.Where == nil || q.Where.SQL != tt.where {
					t.Fatalf("where = %+v, want %q", q.Where, tt.where)
				}
				if got := strings.Count(q.Where.SQL, "?"); got != len(q.Where.Args) {
					t.Fatalf("%d placeholders for %d args", got, len(q.Where.Args))
				}
				if !field.Sortable {
					if tt.order != "" {
						t.Fatalf("order %q given for a field that is not sortable", tt.order)
					}
					return
				}
				q, err = s.schema.Compile("", field.Name+" desc")
				if err != nil {
					t.Fatalf("Compile order: %v", err)
				}
				if want := tt.order + " DESC NULLS LAST, " + s.schema.Tiebreak; q.Order != want {
					t.Fatalf("order = %q, want %q", q.Order, want)
				}
			})
		}
		for field := range s.fields {
			found := false
			for _, f := range s.schema.Fields {
				found = found || f.Name == field
			}
			if !found {
				t.Errorf("%s has a case for unknown field %q", name, field)
			}
		}
	}
}

func TestAdminSchemasRejectBadInput(t *testing.T) {
	tests := map[string]struct {
		schema  *adminquery.Schema
		filter  string
		orderBy string
		field   string
	}{
		"a member that is not an id": {schema: roomSchema, filter: "member = ada", field: "filter"},
		"a room that is not an id":   {schema: uploadSchema, filter: "room = lobby", field: "filter"},
		"a user compared by range":   {schema: callSchema, filter: "user > " + someID, field: "filter"},
		"an unknown call state":      {schema: callSchema, filter: "state = paused", field: "filter"},
		"an unknown room kind":       {schema: roomSchema, filter: "kind = channel", field: "filter"},
		"a mode compared by range":   {schema: callSchema, filter: "mode < video", field: "filter"},
		"sorting by a member":        {schema: roomSchema, orderBy: "member", field: "order_by"},
		"sorting by an uploader":     {schema: uploadSchema, orderBy: "uploader desc", field: "order_by"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := tt.schema.Compile(tt.filter, tt.orderBy)
			var qe *adminquery.Error
			if !errors.As(err, &qe) || qe.Field != tt.field {
				t.Fatalf("err = %v, want an %s error", err, tt.field)
			}
		})
	}
}
