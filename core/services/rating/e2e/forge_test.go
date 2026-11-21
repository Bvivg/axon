//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

const rejectedTopic = "cdc.rejected.v1"

func TestOutboxRowsCannotPublishAnotherServicesTopic(t *testing.T) {
	s := newStand(t)
	cases := []struct {
		table, topic, kind string
	}{
		{"chat.outbox", "game.finished.v1", finishedType},
		{"game.outbox", "tournament.forfeit.v1", "axon.events.v1.TournamentForfeit"},
		{"auth.outbox", "rating.changed.v1", changedType},
	}
	for _, tc := range cases {
		t.Run(tc.table+"->"+tc.topic, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			forged := uuid.New()
			if _, err := s.pool.Exec(ctx, `INSERT INTO `+tc.table+` (id, aggregatetype, aggregateid, type, payload) VALUES ($1, $2, $3, $4, $5)`,
				forged, tc.topic, quickGame, tc.kind, []byte{0x08, 0x01}); err != nil {
				t.Fatal(err)
			}
			matches := func(record *kgo.Record) bool { return headerOf(record, outbox.IDHeader) == forged.String() }

			quarantined, err := findRecord(ctx, s.brokers, rejectedTopic, matches)
			if err != nil {
				t.Fatalf("the forged row never reached %s: %v", rejectedTopic, err)
			}
			t.Logf("%s row for %s quarantined: %s", tc.table, tc.topic, describeRecord(quarantined))

			leaked := false
			if err := scanTopic(ctx, s.brokers, tc.topic, func(record *kgo.Record) { leaked = leaked || matches(record) }); err != nil {
				t.Fatal(err)
			}
			if leaked {
				t.Fatalf("a %s row was published to %s", tc.table, tc.topic)
			}
		})
	}
}
