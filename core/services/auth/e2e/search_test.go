//go:build e2e

package e2e

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

func (c *caller) searchUsers(accessToken, query string) ([]*authv1.PublicProfile, error) {
	r := connect.NewRequest(&authv1.SearchUsersRequest{Query: query})
	r.Header().Set(authn.Header, "Bearer "+accessToken)

	resp, err := c.client.SearchUsers(context.Background(), r)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetUsers(), nil
}

func (c *caller) getUsersPublicProfiles(accessToken string, userIDs []string) ([]*authv1.PublicProfile, error) {
	r := connect.NewRequest(&authv1.GetUsersPublicProfilesRequest{UserIds: userIDs})
	r.Header().Set(authn.Header, "Bearer "+accessToken)

	resp, err := c.client.GetUsersPublicProfiles(context.Background(), r)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetUsers(), nil
}

func TestSearchUsersDoesNotConfirmAnExactEmail(t *testing.T) {
	c := newCaller(t)
	caller := c.register(t)
	target := c.register(t)

	found, err := c.searchUsers(caller.tokens.GetAccessToken(), target.email)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want no account confirmed by its address", found)
	}
}

func TestSearchUsersRefusesAQueryTooShortToNarrowTheDirectory(t *testing.T) {
	c := newCaller(t)
	caller := c.register(t)

	for _, query := range []string{"", "a", "@"} {
		if _, err := c.searchUsers(caller.tokens.GetAccessToken(), query); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("SearchUsers(%q) code = %v, want invalid_argument", query, connect.CodeOf(err))
		}
	}
}

func TestSearchUsersByGarbageFindsNothing(t *testing.T) {
	c := newCaller(t)
	caller := c.register(t)

	found, err := c.searchUsers(caller.tokens.GetAccessToken(), "nobody-"+uuid.NewString()+"@axon.test")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want empty", found)
	}
}

func TestSearchUsersExcludesTheCallerByEmailAndName(t *testing.T) {
	c := newCaller(t)
	caller := c.register(t)

	byEmail, err := c.searchUsers(caller.tokens.GetAccessToken(), caller.email)
	if err != nil {
		t.Fatalf("SearchUsers by own email: %v", err)
	}
	if len(byEmail) != 0 {
		t.Fatalf("found = %v, want no results searching for yourself by email", byEmail)
	}

	first, last := "Self", "Search-"+uuid.NewString()
	if _, err := c.updateProfile(caller.tokens.GetAccessToken(), &authv1.UpdateProfileRequest{
		FirstName: &first, LastName: &last,
	}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	byName, err := c.searchUsers(caller.tokens.GetAccessToken(), first+" "+last)
	if err != nil {
		t.Fatalf("SearchUsers by own name: %v", err)
	}
	if len(byName) != 0 {
		t.Fatalf("found = %v, want no results searching for yourself by display name", byName)
	}
}

func TestGetUsersPublicProfilesReturnsOnlyKnownIDs(t *testing.T) {
	c := newCaller(t)
	caller := c.register(t)
	known := c.register(t)

	found, err := c.getUsersPublicProfiles(caller.tokens.GetAccessToken(),
		[]string{known.userID, uuid.NewString()})
	if err != nil {
		t.Fatalf("GetUsersPublicProfiles: %v", err)
	}
	if len(found) != 1 || found[0].GetId() != known.userID {
		t.Fatalf("found = %v, want exactly %s", found, known.userID)
	}
}

func TestSearchUsersFindsTheOtherAccountByANameFragment(t *testing.T) {
	c := newCaller(t)
	caller := c.register(t)
	target := c.register(t)

	first, last := "Fragment", "Target-"+uuid.NewString()
	if _, err := c.updateProfile(target.tokens.GetAccessToken(), &authv1.UpdateProfileRequest{
		FirstName: &first, LastName: &last,
	}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	found, err := c.searchUsers(caller.tokens.GetAccessToken(), last[3:20])
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 1 || found[0].GetId() != target.userID {
		t.Fatalf("found = %v, want exactly %s", found, target.userID)
	}
}
