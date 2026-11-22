package server_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/services/rating/internal/server"
	"github.com/bvivg/axon/core/services/rating/internal/store"
	ratingv1 "github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

type adminStore struct {
	rows  []store.RatingRow
	next  string
	err   error
	asked store.RatingFilter
}

func (s *adminStore) Changes(context.Context, store.ChangeFilter) ([]store.Change, string, error) {
	return nil, "", nil
}

func (s *adminStore) Ratings(_ context.Context, f store.RatingFilter) ([]store.RatingRow, string, error) {
	s.asked = f
	return s.rows, s.next, s.err
}

func TestListRatings(t *testing.T) {
	updated := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	rows := []store.RatingRow{{Game: "chess", Player: detailed(alice, 1612.25, 4), UpdatedAt: updated}, {Game: "chess", Player: detailed(bob, 1410, 2), UpdatedAt: updated}}
	tests := []struct {
		name    string
		request *ratingv1.ListRatingsRequest
		store   *adminStore
		code    connect.Code
		asked   store.RatingFilter
		users   []string
		next    string
	}{
		{
			name:    "default page of every game",
			request: &ratingv1.ListRatingsRequest{},
			store:   &adminStore{rows: rows, next: "cursor"},
			asked:   store.RatingFilter{Limit: server.DefaultLimit},
			users:   []string{alice.String(), bob.String()},
			next:    "cursor",
		},
		{
			name:    "filters pass through and the page is capped",
			request: &ratingv1.ListRatingsRequest{Game: "chess", UserId: alice.String(), Cursor: "abc", PageSize: 1000},
			store:   &adminStore{rows: rows[:1]},
			asked:   store.RatingFilter{Game: "chess", User: &alice, Cursor: "abc", Limit: server.MaxLimit},
			users:   []string{alice.String()},
		},
		{name: "unknown game key", request: &ratingv1.ListRatingsRequest{Game: "Chess; DROP"}, store: &adminStore{}, code: connect.CodeInvalidArgument},
		{name: "malformed user", request: &ratingv1.ListRatingsRequest{UserId: "alice"}, store: &adminStore{}, code: connect.CodeInvalidArgument},
		{name: "broken cursor", request: &ratingv1.ListRatingsRequest{Cursor: "%%%"}, store: &adminStore{err: store.ErrInvalidCursor}, code: connect.CodeInvalidArgument},
		{name: "store down", request: &ratingv1.ListRatingsRequest{}, store: &adminStore{err: errors.New("postgres down")}, code: connect.CodeUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := server.NewAdmin(tt.store, logger.Discard()).ListRatings(context.Background(), connect.NewRequest(tt.request))
			if tt.code != 0 {
				if connect.CodeOf(err) != tt.code {
					t.Fatalf("code = %v, want %v (%v)", connect.CodeOf(err), tt.code, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			asked := tt.store.asked
			if asked.Game != tt.asked.Game || asked.Cursor != tt.asked.Cursor || asked.Limit != tt.asked.Limit ||
				(asked.User == nil) != (tt.asked.User == nil) || asked.User != nil && *asked.User != *tt.asked.User {
				t.Fatalf("asked %+v, want %+v", asked, tt.asked)
			}
			if response.Msg.NextCursor != tt.next || len(response.Msg.Ratings) != len(tt.users) {
				t.Fatalf("response %v", response.Msg)
			}
			for i, entry := range response.Msg.Ratings {
				if entry.UserId != tt.users[i] || entry.Game != "chess" || !entry.UpdatedAt.AsTime().Equal(updated) {
					t.Fatalf("entry %d = %v", i, entry)
				}
			}
			first := response.Msg.Ratings[0]
			if first.Rating != rows[0].Player.Rating.Rating || first.Deviation != 120.5 || first.Volatility != 0.06 || first.Games != 4 || first.Wins != 4 {
				t.Fatalf("first entry = %v", first)
			}
		})
	}
}
