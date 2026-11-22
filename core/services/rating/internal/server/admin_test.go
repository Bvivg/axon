package server_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/rating/internal/server"
	"github.com/bvivg/axon/core/services/rating/internal/store"
	ratingv1 "github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1"
	"github.com/bvivg/axon/core/shared/pkg/adminquery"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

type adminStore struct {
	rows  []store.RatingRow
	next  string
	total *int64
	err   error
	asked store.RatingFilter
}

func (s *adminStore) Changes(context.Context, store.ChangeFilter) (store.ChangePage, error) {
	return store.ChangePage{}, nil
}

func (s *adminStore) Ratings(_ context.Context, f store.RatingFilter) (store.RatingPage, error) {
	s.asked = f
	return store.RatingPage{Ratings: s.rows, Next: s.next, Total: s.total}, s.err
}

func TestListRatings(t *testing.T) {
	updated := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	rows := []store.RatingRow{{Game: "chess", Player: detailed(alice, 1612.25, 4), UpdatedAt: updated}, {Game: "chess", Player: detailed(bob, 1410, 2), UpdatedAt: updated}}
	total, offset := int64(7), int32(25)
	paged := adminquery.Request{Filter: "rating >= 1400", OrderBy: "games desc", Offset: &offset, PageSize: 25}
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
			asked:   store.RatingFilter{Game: "chess", User: &alice, Cursor: "abc", Limit: server.MaxLimit, List: adminquery.Request{PageSize: 1000}},
			users:   []string{alice.String()},
		},
		{
			name:    "an offset page carries the query and the total",
			request: &ratingv1.ListRatingsRequest{Game: "chess", Filter: proto.String(paged.Filter), OrderBy: proto.String(paged.OrderBy), Offset: &offset, PageSize: 25},
			store:   &adminStore{rows: rows, total: &total},
			asked:   store.RatingFilter{Game: "chess", Limit: 25, List: paged},
			users:   []string{alice.String(), bob.String()},
		},
		{
			name:    "an invalid query is the caller's mistake",
			request: &ratingv1.ListRatingsRequest{Filter: proto.String("wins > 2")},
			store:   &adminStore{err: &adminquery.Error{Field: "filter", Message: `cannot filter by "wins"`}},
			code:    connect.CodeInvalidArgument,
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
				(asked.User == nil) != (tt.asked.User == nil) || asked.User != nil && *asked.User != *tt.asked.User ||
				asked.List.Filter != tt.asked.List.Filter || asked.List.OrderBy != tt.asked.List.OrderBy || asked.List.PageSize != tt.asked.List.PageSize ||
				asked.List.Paged() != tt.asked.List.Paged() || asked.List.Paged() && *asked.List.Offset != *tt.asked.List.Offset {
				t.Fatalf("asked %+v, want %+v", asked, tt.asked)
			}
			if (response.Msg.Total == nil) != (tt.store.total == nil) || tt.store.total != nil && response.Msg.GetTotal() != *tt.store.total {
				t.Fatalf("total = %v, want %v", response.Msg.Total, tt.store.total)
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
