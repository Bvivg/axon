package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	"github.com/bvivg/axon/core/shared/pkg/adminquery"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
)

type AdminFilter struct {
	Query string
	List  adminquery.Request
}

type AdminPage struct {
	Tournaments []Summary
	Total       int64
}

const registeredPlayers = `(SELECT count(*) FROM tournament.registrations r WHERE r.tournament_id = t.id AND NOT r.returned)`

func oneOf[T ~string](column string, values ...T) map[string]string {
	out := make(map[string]string, len(values))
	for _, v := range values {
		out[string(v)] = column + " = '" + string(v) + "'"
	}
	return out
}

var tournamentSchema = adminquery.NewSchema("t.created_at DESC, t.id DESC", "t.id DESC",
	adminquery.Field{Name: "id", Column: "t.id", Kind: adminquery.UUID},
	adminquery.Field{Name: "name", Column: "t.name", Kind: adminquery.String, Sortable: true},
	adminquery.Field{Name: "game", Column: "t.game", Kind: adminquery.String},
	adminquery.Field{Name: "status", Kind: adminquery.Enum, Values: oneOf("t.status", saga.Registration, saga.Seeding, saga.Running, saga.Finished, saga.Cancelled)},
	adminquery.Field{Name: "format", Kind: adminquery.Enum, Values: oneOf("t.format", saga.SingleElimination, saga.Swiss)},
	adminquery.Field{Name: "players", Column: registeredPlayers, Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "capacity", Column: "t.capacity", Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "created", Column: "t.created_at", Kind: adminquery.Time, Sortable: true},
	adminquery.Field{Name: "started", Column: "t.started_at", Kind: adminquery.Time, Sortable: true},
	adminquery.Field{Name: "creator", Column: "t.creator_id", Kind: adminquery.UUID},
)

func (s *Postgres) AdminList(ctx context.Context, f AdminFilter) (AdminPage, error) {
	var where adminquery.Where
	if q := strings.TrimSpace(f.Query); q != "" {
		where.Add(`t.name ILIKE ? ESCAPE '\'`, postgres.Containing(q))
	}
	listed, err := tournamentSchema.Prepare(f.List, &where)
	if err != nil {
		return AdminPage{}, err
	}
	var page AdminPage
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tournament.tournaments t`+where.SQL(), where.Args()...).Scan(&page.Total); err != nil {
		return AdminPage{}, fmt.Errorf("store: count tournaments: %w", err)
	}
	limit, offset := f.List.Page()
	rows, err := s.pool.Query(ctx, `SELECT `+tournamentColumns+`, `+registeredPlayers+`
		FROM tournament.tournaments t`+where.SQL()+`
		ORDER BY `+listed.Order+where.Paginate(limit, offset), where.Args()...)
	if err != nil {
		return AdminPage{}, fmt.Errorf("store: list tournaments: %w", err)
	}
	page.Tournaments, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Summary, error) {
		var out Summary
		t, err := scanTournament(row, &out.Players)
		out.Tournament = t
		return out, err
	})
	if err != nil {
		return AdminPage{}, fmt.Errorf("store: list tournaments: %w", err)
	}
	return page, nil
}
