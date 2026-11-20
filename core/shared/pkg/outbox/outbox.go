package outbox

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"
)

type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type Event struct {
	ID          uuid.UUID
	Topic       string
	AggregateID string
	Payload     proto.Message
}

var ErrInvalid = errors.New("outbox: invalid event")

var tableName = regexp.MustCompile(`^[a-z_]+\.outbox$`)

const TypeHeader = "event_type"

const IDHeader = "event_id"

func Write(ctx context.Context, tx Execer, table string, events ...Event) error {
	if !tableName.MatchString(table) {
		return fmt.Errorf("%w: table %q", ErrInvalid, table)
	}
	for _, event := range events {
		if event.ID == uuid.Nil || event.Topic == "" || event.AggregateID == "" || event.Payload == nil {
			return fmt.Errorf("%w: %+v", ErrInvalid, event)
		}
		payload, err := proto.Marshal(event.Payload)
		if err != nil {
			return fmt.Errorf("outbox: marshal %s: %w", event.Topic, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO `+table+` (id, aggregatetype, aggregateid, type, payload) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO NOTHING`,
			event.ID, event.Topic, event.AggregateID, string(event.Payload.ProtoReflect().Descriptor().FullName()), payload); err != nil {
			return fmt.Errorf("outbox: insert %s: %w", event.Topic, err)
		}
	}
	return nil
}

func Cleanup(ctx context.Context, db Execer, table string, olderThan time.Duration) (int64, error) {
	if !tableName.MatchString(table) {
		return 0, fmt.Errorf("%w: table %q", ErrInvalid, table)
	}
	tag, err := db.Exec(ctx, `DELETE FROM `+table+` WHERE created_at < clock_timestamp() - $1::interval`, olderThan.String())
	if err != nil {
		return 0, fmt.Errorf("outbox: cleanup %s: %w", table, err)
	}
	return tag.RowsAffected(), nil
}

func Migration(schema string) string {
	return `CREATE TABLE ` + schema + `.outbox (
    id uuid PRIMARY KEY,
    aggregatetype text NOT NULL,
    aggregateid text NOT NULL,
    type text NOT NULL,
    payload bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX ` + schema + `_outbox_created_at ON ` + schema + `.outbox (created_at);`
}
