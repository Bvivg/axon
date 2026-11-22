package admincrud

import (
	"errors"
	"testing"

	validatepb "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/pkg/authz"
)

func TestEtag(t *testing.T) {
	cases := []struct {
		raw     string
		version int64
		valid   bool
	}{
		{Etag(7), 7, true},
		{`"3"`, 3, true},
		{"12", 12, true},
		{"", 0, false},
		{`W/"0"`, 0, false},
		{`W/"x"`, 0, false},
	}
	for _, c := range cases {
		version, err := ParseEtag(c.raw)
		if c.valid != (err == nil) || version != c.version {
			t.Errorf("ParseEtag(%q) = %d, %v", c.raw, version, err)
		}
		if !c.valid && connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("ParseEtag(%q) code = %v", c.raw, connect.CodeOf(err))
		}
	}
}

func TestConflict(t *testing.T) {
	err := Conflict(4)
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("code = %v", connect.CodeOf(err))
	}
	info := detail[*errdetails.ErrorInfo](t, err)
	if info.GetReason() != ReasonEtagMismatch || info.GetMetadata()["etag"] != `W/"4"` {
		t.Fatalf("info = %v", info)
	}
}

func TestPaths(t *testing.T) {
	fields := Fields{"title": authz.RolesMutate, "permissions": authz.RolesMutate, "secret": authz.UsersMutate}
	granted := authz.NewSet([]string{authz.RolesMutate.String()})
	cases := []struct {
		name  string
		mask  []string
		want  []string
		code  connect.Code
		field string
	}{
		{"empty", nil, nil, connect.CodeInvalidArgument, "update_mask"},
		{"unknown", []string{"name"}, nil, connect.CodeInvalidArgument, "update_mask"},
		{"denied", []string{"title", "secret"}, nil, connect.CodePermissionDenied, ""},
		{"sorted and unique", []string{"title", "permissions", "title"}, []string{"permissions", "title"}, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var mask *fieldmaskpb.FieldMask
			if c.mask != nil {
				mask = &fieldmaskpb.FieldMask{Paths: c.mask}
			}
			got, err := fields.Paths(mask, granted)
			if c.code != 0 {
				if connect.CodeOf(err) != c.code {
					t.Fatalf("code = %v, want %v", connect.CodeOf(err), c.code)
				}
				if c.field != "" && violationField(t, err) != c.field {
					t.Fatalf("field = %q", violationField(t, err))
				}
				return
			}
			if err != nil || len(got) != len(c.want) {
				t.Fatalf("Paths = %v, %v", got, err)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("Paths = %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestDiff(t *testing.T) {
	before := &authv1.AdminRole{Name: "ops", Title: "Ops", Permissions: []string{"b", "a"}, MemberCount: 2}
	after := &authv1.AdminRole{Name: "ops", Title: "Operations", Permissions: []string{"a", "b"}, MemberCount: 3}
	changes := Diff(before, after, []string{"name", "title", "permissions", "member_count"}, "member_count")
	want := []authz.Change{
		{Field: "title", Before: "Ops", After: "Operations"},
		{Field: "member_count", Before: Redacted, After: Redacted},
	}
	if len(changes) != len(want) {
		t.Fatalf("changes = %+v", changes)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Fatalf("changes[%d] = %+v, want %+v", i, changes[i], want[i])
		}
	}
	added := Diff(&authv1.AdminRole{}, after, []string{"permissions"})
	if len(added) != 1 || added[0].Before != "" || added[0].After != "a, b" {
		t.Fatalf("added = %+v", added)
	}
}

func TestSnapshot(t *testing.T) {
	changes := Snapshot(&authv1.AdminRole{Name: "ops", Permissions: []string{"x"}, Etag: "W/\"1\""}, "etag")
	if len(changes) != 3 || changes[0].Field != "name" || changes[2].Before != Redacted {
		t.Fatalf("snapshot = %+v", changes)
	}
}

func TestFieldError(t *testing.T) {
	err := Taken("name", "a role with this name exists")
	if connect.CodeOf(err) != connect.CodeAlreadyExists || violationField(t, err) != "name" {
		t.Fatalf("err = %v", err)
	}
}

func TestUniqueViolation(t *testing.T) {
	if UniqueViolation(errors.New("boom"), "") {
		t.Fatal("plain error is not a unique violation")
	}
}

func detail[T any](t *testing.T, err error) T {
	t.Helper()
	var ce *connect.Error
	if !errors.As(err, &ce) {
		t.Fatalf("not a connect error: %v", err)
	}
	for _, d := range ce.Details() {
		value, derr := d.Value()
		if derr != nil {
			continue
		}
		if typed, ok := value.(T); ok {
			return typed
		}
	}
	var zero T
	t.Fatalf("no %T detail in %v", zero, err)
	return zero
}

func violationField(t *testing.T, err error) string {
	t.Helper()
	violations := detail[*validatepb.Violations](t, err)
	return violations.GetViolations()[0].GetField().GetElements()[0].GetFieldName()
}
