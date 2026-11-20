package tournamentid_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/tournamentid"
)

func TestSessionIsAStableVersion5UUIDPerRoundAndBoard(t *testing.T) {
	tournament := uuid.MustParse("0b4fb8d3-0d0e-4c43-8f39-2a6b8f5e1e11")
	first := tournamentid.Session(tournament, 1, 1)
	if first != tournamentid.Session(tournament, 1, 1) {
		t.Fatal("the same board must map to the same session")
	}
	if first.Version() != 5 || first.Variant() != uuid.RFC4122 {
		t.Fatalf("got version %d variant %v", first.Version(), first.Variant())
	}
	seen := map[uuid.UUID]bool{first: true}
	for _, id := range []uuid.UUID{
		tournamentid.Session(tournament, 1, 2),
		tournamentid.Session(tournament, 2, 1),
		tournamentid.Session(uuid.New(), 1, 1),
	} {
		if seen[id] {
			t.Fatalf("collision on %s", id)
		}
		seen[id] = true
	}
}
