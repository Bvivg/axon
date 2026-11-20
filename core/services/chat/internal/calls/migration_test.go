package calls

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func migrationFixture(t *testing.T) (Call, []uuid.UUID, []EndpointRef, time.Time) {
	t.Helper()
	c, ea, eb, now := fixture(t)
	users := []uuid.UUID{c.Participants[0].UserID, c.Participants[1].UserID}
	refs := []EndpointRef{ea, eb}
	c.Participants[0].SupportsSFU = true
	c.Participants[0].SupportsMigration = true
	if _, err := Apply(&c, users[1], Command{Action: "accept", Endpoint: eb, SFUCapable: true, MigrationCapable: true, EnableSFU: true}, now); err != nil {
		t.Fatal(err)
	}
	for i, u := range users {
		if _, err := Apply(&c, u, Command{Action: "ready", Endpoint: refs[i], Generation: 1, Ready: true}, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Apply(&c, users[0], Command{Action: "migrate", Endpoint: ea, Generation: 1, EnableSFU: true, EnableMigration: true}, now); err != nil {
		t.Fatal(err)
	}
	return c, users, refs, now
}
func reportMigration(t *testing.T, c *Call, user uuid.UUID, ref EndpointRef, action string, now time.Time) {
	t.Helper()
	peer := c.Migration.Peer(user)
	changed, err := Apply(c, user, Command{Action: action, Endpoint: ref, MigrationID: c.Migration.ID, Generation: c.Migration.TargetGeneration, MediaVersion: peer.MediaVersion, Ready: true}, now)
	if err != nil || !changed {
		t.Fatalf("%s changed=%v err=%v", action, changed, err)
	}
}
func TestMigrationCommitsOnlyAfterBothSwitchedAndKeepsBusinessIdentity(t *testing.T) {
	c, users, refs, now := migrationFixture(t)
	id, start, version := c.ID, *c.StartedAt, c.Version
	if c.State != Active || c.TransportKind() != "p2p" || c.Generation != 1 || c.Migration.TargetGeneration != 2 {
		t.Fatal("primary changed during preparation")
	}
	for i, u := range users {
		reportMigration(t, &c, u, refs[i], "migration_ready", now)
		if i == 0 && c.Migration.Phase != "preparing" {
			t.Fatal("one ready committed")
		}
	}
	if c.Migration.Phase != "committing" || c.Generation != 1 {
		t.Fatal("ready must not commit primary")
	}
	for i, u := range users {
		reportMigration(t, &c, u, refs[i], "migration_switched", now)
		if i == 0 && c.TransportKind() != "p2p" {
			t.Fatal("one switched committed")
		}
	}
	if c.TransportKind() != "sfu" || c.Generation != 2 || c.State != Active || c.ID != id || !c.StartedAt.Equal(start) || !c.EverActive || c.Migration.Phase != "cleaning" {
		t.Fatal("commit changed business identity or lost cleanup barrier")
	}
	for i, u := range users {
		reportMigration(t, &c, u, refs[i], "migration_clean", now)
	}
	if c.Migration.Phase != "completed" || !c.Deadline.IsZero() || c.Migration.FinishedAt == nil || c.Version <= version {
		t.Fatal("migration not completed")
	}
}
func TestMigrationRollbackBeforeCommitRetainsLiveSourceAndNeverReusesGeneration(t *testing.T) {
	c, users, refs, now := migrationFixture(t)
	oldOperation := c.Migration.ID
	for i, u := range users {
		reportMigration(t, &c, u, refs[i], "migration_ready", now)
	}
	reportMigration(t, &c, users[0], refs[0], "migration_switched", now)
	at := c.Migration.Deadline
	if _, err := Apply(&c, uuid.Nil, Command{Action: "timeout"}, at); err != nil {
		t.Fatal(err)
	}
	if c.Migration.Phase != "aborting" || c.TransportKind() != "p2p" || c.Generation != 1 {
		t.Fatal("timeout committed or destroyed source")
	}
	for i, u := range users {
		reportMigration(t, &c, u, refs[i], "migration_abort_clean", at)
	}
	if c.Migration.Phase != "aborted" || c.State != Active || !c.Deadline.IsZero() {
		t.Fatal("healthy rollback did not retain active source")
	}
	if _, err := Apply(&c, users[0], Command{Action: "migrate", Endpoint: refs[0], Generation: 1, EnableSFU: true, EnableMigration: true}, at); err != nil {
		t.Fatal(err)
	}
	if c.Migration.ID == oldOperation || c.Migration.TargetGeneration != 3 {
		t.Fatal("aborted grant generation reused")
	}
	if _, err := Apply(&c, users[1], Command{Action: "migration_switched", Endpoint: refs[1], MigrationID: oldOperation, Generation: 2, MediaVersion: 1}, at); !errors.Is(err, ErrStale) {
		t.Fatalf("old operation accepted: %v", err)
	}
}
func TestMigrationEndAndRevocationFenceLateSwitches(t *testing.T) {
	for _, action := range []string{"end", "endpoint_expired"} {
		t.Run(action, func(t *testing.T) {
			c, users, refs, now := migrationFixture(t)
			for i, u := range users {
				reportMigration(t, &c, u, refs[i], "migration_ready", now)
			}
			reportMigration(t, &c, users[0], refs[0], "migration_switched", now)
			cmd := Command{Action: "end", Endpoint: refs[0]}
			actor := users[0]
			if action == "endpoint_expired" {
				cmd = Command{Action: "timeout", Reason: action}
				actor = uuid.Nil
			}
			if _, err := Apply(&c, actor, cmd, now); err != nil {
				t.Fatal(err)
			}
			if _, err := Apply(&c, users[1], Command{Action: "migration_switched", Endpoint: refs[1], MigrationID: c.Migration.ID, Generation: 2, MediaVersion: 1}, now); !errors.Is(err, ErrStale) {
				t.Fatalf("late switch %v", err)
			}
			if c.State != Ended || c.Migration.Phase != "aborted" || c.TransportKind() != "p2p" {
				t.Fatal("terminal call resurrected")
			}
		})
	}
}
func TestMigrationCleanupTimeoutAfterCommitCannotRollbackEndedSource(t *testing.T) {
	c, users, refs, now := migrationFixture(t)
	for _, action := range []string{"migration_ready", "migration_switched"} {
		for i, u := range users {
			reportMigration(t, &c, u, refs[i], action, now)
		}
	}
	if _, err := Apply(&c, uuid.Nil, Command{Action: "timeout"}, c.Migration.Deadline); err != nil {
		t.Fatal(err)
	}
	if c.State != Ended || c.TransportKind() != "sfu" || c.Generation != 2 || c.EndReason != "migration_cleanup_timeout" {
		t.Fatal("unproven cleanup resurrected source")
	}
}
func TestMigrationRejectsWrongEpochVersionAndDeadline(t *testing.T) {
	c, users, refs, now := migrationFixture(t)
	base := Command{Action: "migration_ready", Endpoint: refs[0], MigrationID: c.Migration.ID, Generation: 2, MediaVersion: 1}
	for _, bad := range []Command{
		{Action: base.Action, Endpoint: EndpointRef{ID: refs[0].ID, Epoch: 2}, MigrationID: base.MigrationID, Generation: 2, MediaVersion: 1},
		{Action: base.Action, Endpoint: refs[0], MigrationID: base.MigrationID, Generation: 2, MediaVersion: 2},
		{Action: base.Action, Endpoint: refs[0], MigrationID: uuid.New(), Generation: 2, MediaVersion: 1},
	} {
		if _, err := Apply(&c, users[0], bad, now); err == nil {
			t.Fatal("stale report accepted")
		}
	}
	if _, err := Apply(&c, users[0], base, c.Migration.Deadline); !errors.Is(err, ErrConflict) {
		t.Fatalf("deadline fence=%v", err)
	}
	if c.Migration.Peers[0].Ready {
		t.Fatal("invalid report changed readiness")
	}
}
func TestMigrationEndpointResumeConsumesReservedGeneration(t *testing.T) {
	c, users, refs, now := migrationFixture(t)
	refs[0].Epoch++
	if _, err := Apply(&c, users[0], Command{Action: "resume", Endpoint: refs[0], SFUCapable: true, MigrationCapable: true, EnableSFU: true}, now); err != nil {
		t.Fatal(err)
	}
	if c.Generation != 3 || c.Migration.Phase != "aborted" || c.State != Connecting {
		t.Fatal("endpoint resume reused a candidate grant or retained stale migration")
	}
}

func TestMigrationCommittedCleanupDeadlineSurvivesNativeReadyAndReconnect(t *testing.T) {
	for _, action := range []string{"ready", "reconnecting", "resume"} {
		t.Run(action, func(t *testing.T) {
			c, users, refs, now := migrationFixture(t)
			for _, report := range []string{"migration_ready", "migration_switched"} {
				for i, user := range users {
					reportMigration(t, &c, user, refs[i], report, now)
				}
			}
			deadline := c.Deadline
			ref := refs[0]
			if action == "resume" {
				ref.Epoch++
			}
			_, err := Apply(&c, users[0], Command{Action: action, Endpoint: ref, Generation: c.Generation, MediaVersion: c.Participants[0].MediaVersion, Ready: true}, now)
			if err != nil {
				t.Fatal(err)
			}
			if action == "ready" {
				if c.Deadline != deadline || c.Migration.Phase != "cleaning" {
					t.Fatal("native ready erased the pending cleanup deadline")
				}
			} else if c.State != Ended {
				t.Fatal("post-commit reconnect resumed an unclean migration")
			}
		})
	}
}
