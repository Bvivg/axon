//go:build e2e

package e2e

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

var usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)

func newUsername() string {
	return "e2e_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
}

func (c *caller) registerWithUsername(username string) (*authv1.RegisterResponse, error) {
	resp, err := c.client.Register(context.Background(), connect.NewRequest(&authv1.RegisterRequest{
		Email:    email(),
		Password: testPassword,
		Username: &username,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (c *caller) publicProfileByUsername(accessToken, username string) (*authv1.PublicProfile, error) {
	r := connect.NewRequest(&authv1.GetPublicProfileByUsernameRequest{Username: username})
	if accessToken != "" {
		r.Header().Set(authn.Header, "Bearer "+accessToken)
	}

	resp, err := c.client.GetPublicProfileByUsername(context.Background(), r)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetUser(), nil
}

func TestRegisterWithAChosenUsername(t *testing.T) {
	c := newCaller(t)
	name := newUsername()

	created, err := c.registerWithUsername("@" + strings.ToUpper(name))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := created.GetUser().GetUsername(); got != name {
		t.Fatalf("username = %q, want %q", got, name)
	}

	_, err = c.registerWithUsername(name)
	requireCode(t, err, connect.CodeAlreadyExists)

	_, err = c.registerWithUsername("9" + name)
	requireCode(t, err, connect.CodeInvalidArgument)
}

func TestRegisterWithoutAUsernameGetsOne(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	me, err := c.getMe(acct.tokens.GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if !usernamePattern.MatchString(me.GetUsername()) {
		t.Fatalf("username = %q, want a generated one", me.GetUsername())
	}
}

func TestAUsernameFindsItsPerson(t *testing.T) {
	c := newCaller(t)
	viewer := c.register(t)
	name := newUsername()
	target, err := c.registerWithUsername(name)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	profile, err := c.publicProfileByUsername(viewer.tokens.GetAccessToken(), "@"+name)
	if err != nil {
		t.Fatalf("GetPublicProfileByUsername: %v", err)
	}
	if profile.GetId() != target.GetUser().GetId() || profile.GetUsername() != name {
		t.Fatalf("profile = %v, want %s", profile, target.GetUser().GetId())
	}

	found, err := c.searchUsers(viewer.tokens.GetAccessToken(), "@"+name)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 1 || found[0].GetId() != target.GetUser().GetId() {
		t.Fatalf("found = %v, want exactly %s", found, target.GetUser().GetId())
	}

	_, err = c.publicProfileByUsername(viewer.tokens.GetAccessToken(), newUsername())
	requireCode(t, err, connect.CodeNotFound)

	_, err = c.publicProfileByUsername("", name)
	requireCode(t, err, connect.CodeUnauthenticated)
}

func TestAUsernameCanBeChangedButNotTaken(t *testing.T) {
	c := newCaller(t)
	ada := c.register(t)
	eve := c.register(t)
	name := newUsername()

	updated, err := c.updateProfile(ada.tokens.GetAccessToken(), &authv1.UpdateProfileRequest{Username: &name})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.GetUsername() != name {
		t.Fatalf("username = %q, want %q", updated.GetUsername(), name)
	}

	_, err = c.updateProfile(eve.tokens.GetAccessToken(), &authv1.UpdateProfileRequest{Username: &name})
	requireCode(t, err, connect.CodeAlreadyExists)

	blank := ""
	_, err = c.updateProfile(ada.tokens.GetAccessToken(), &authv1.UpdateProfileRequest{Username: &blank})
	requireCode(t, err, connect.CodeInvalidArgument)
}
