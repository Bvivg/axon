//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"testing"

	validatepb "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
)

func violatedField(t *testing.T, err error) string {
	t.Helper()
	var ce *connect.Error
	if !errors.As(err, &ce) {
		t.Fatalf("not a connect error: %v", err)
	}
	for _, d := range ce.Details() {
		if value, derr := d.Value(); derr == nil {
			if violations, ok := value.(*validatepb.Violations); ok && len(violations.GetViolations()) > 0 {
				elements := violations.GetViolations()[0].GetField().GetElements()
				return elements[len(elements)-1].GetFieldName()
			}
		}
	}
	t.Fatalf("no field violation in %v", err)
	return ""
}

func TestAnAdminManagesARoleThroughTheGateway(t *testing.T) {
	c := newCaller(t)
	admin := c.register(t)
	member := c.register(t)
	grantAdminDirectly(t, admin.userID)
	token := admin.tokens.GetAccessToken()
	console := c.admin()
	ctx := context.Background()
	name := "e2e_" + uuid.NewString()[:8]
	request := uuid.NewString()

	_, err := console.CreateRole(ctx, signed(token, &authv1.CreateRoleRequest{Role: &authv1.AdminRole{Name: "Bad Name", Title: "Support"}, Reason: adminReason, RequestId: request}))
	requireCode(t, err, connect.CodeInvalidArgument)
	if field := violatedField(t, err); field != "name" {
		t.Fatalf("invalid name reported on %q", field)
	}
	_, err = console.CreateRole(ctx, signed(token, &authv1.CreateRoleRequest{Role: &authv1.AdminRole{Name: "moderator", Title: "Again", Permissions: []string{"admin.users:read"}}, Reason: adminReason, RequestId: uuid.NewString()}))
	requireCode(t, err, connect.CodeAlreadyExists)
	if field := violatedField(t, err); field != "name" {
		t.Fatalf("duplicate name reported on %q", field)
	}

	created, err := console.CreateRole(ctx, signed(token, &authv1.CreateRoleRequest{Role: &authv1.AdminRole{Name: name, Title: "Support", Permissions: []string{"admin.users:read"}}, Reason: adminReason, RequestId: request}))
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	role := created.Msg.GetRole()
	replayed, err := console.CreateRole(ctx, signed(token, &authv1.CreateRoleRequest{Role: &authv1.AdminRole{Name: name, Title: "Support", Permissions: []string{"admin.users:read"}}, Reason: adminReason, RequestId: request}))
	if err != nil || replayed.Msg.GetRole().GetId() != role.GetId() {
		t.Fatalf("replayed create = %v, %v", replayed, err)
	}

	update := func(etag, title string) (*connect.Response[authv1.UpdateRoleResponse], error) {
		return console.UpdateRole(ctx, signed(token, &authv1.UpdateRoleRequest{
			Role:       &authv1.AdminRole{Id: role.GetId(), Name: name, Title: title, Etag: etag},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
			Reason:     adminReason,
		}))
	}
	first, err := update(role.GetEtag(), "Support, nights")
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	_, err = update(role.GetEtag(), "Support, days")
	requireCode(t, err, connect.CodeAborted)
	second, err := update(first.Msg.GetRole().GetEtag(), "Support, days")
	if err != nil {
		t.Fatalf("UpdateRole with the fresh etag: %v", err)
	}
	dry, err := console.UpdateRole(ctx, signed(token, &authv1.UpdateRoleRequest{
		Role:         &authv1.AdminRole{Id: role.GetId(), Name: name, Title: "Dry run", Etag: second.Msg.GetRole().GetEtag()},
		UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"title"}},
		Reason:       adminReason,
		ValidateOnly: true,
	}))
	if err != nil || dry.Msg.GetRole().GetTitle() != "Dry run" {
		t.Fatalf("validate-only update = %v, %v", dry, err)
	}
	kept, err := console.GetRole(ctx, signed(token, &authv1.GetRoleRequest{RoleId: role.GetId()}))
	if err != nil || kept.Msg.GetRole().GetTitle() != "Support, days" || kept.Msg.GetRole().GetEtag() != second.Msg.GetRole().GetEtag() {
		t.Fatalf("a validate-only update changed the role: %v, %v", kept, err)
	}

	assigned, err := console.SetUserRoles(ctx, signed(token, &authv1.SetUserRolesRequest{UserId: member.userID, Roles: []string{name}, Reason: adminReason}))
	if err != nil || len(assigned.Msg.GetUser().GetUser().GetRoles()) != 1 {
		t.Fatalf("SetUserRoles = %v, %v", assigned, err)
	}
	current, err := console.GetRole(ctx, signed(token, &authv1.GetRoleRequest{RoleId: role.GetId()}))
	if err != nil || current.Msg.GetRole().GetMemberCount() != 1 {
		t.Fatalf("GetRole = %v, %v", current, err)
	}
	_, err = console.DeleteRole(ctx, signed(token, &authv1.DeleteRoleRequest{RoleId: role.GetId(), Etag: current.Msg.GetRole().GetEtag(), Reason: adminReason}))
	requireCode(t, err, connect.CodeFailedPrecondition)
	if _, err := console.SetUserRoles(ctx, signed(token, &authv1.SetUserRolesRequest{UserId: member.userID, Roles: []string{}, Reason: adminReason})); err != nil {
		t.Fatalf("clear roles: %v", err)
	}
	if _, err := console.DeleteRole(ctx, signed(token, &authv1.DeleteRoleRequest{RoleId: role.GetId(), Etag: current.Msg.GetRole().GetEtag(), Reason: adminReason})); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	_, err = console.GetRole(ctx, signed(token, &authv1.GetRoleRequest{RoleId: role.GetId()}))
	requireCode(t, err, connect.CodeNotFound)

	exported, err := console.ExportAuditEvents(ctx, signed(token, &authv1.ExportAuditEventsRequest{Filter: &authv1.ListAuditEventsRequest{TargetType: "role", TargetId: role.GetId()}}))
	if err != nil || !bytes.HasPrefix(exported.Msg.GetCsv(), []byte("occurred_at,")) {
		t.Fatalf("export = %v, %v", exported, err)
	}
}
