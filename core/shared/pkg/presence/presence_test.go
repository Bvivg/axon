package presence

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func TestVisibilityFollowsConnectionsAndExpires(t *testing.T) {
	mr := miniredis.RunT(t)
	tracker := NewTracker(goredis.NewClient(&goredis.Options{Addr: mr.Addr()}))
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tracker.now = func() time.Time { return now }
	ctx := t.Context()

	if err := tracker.TouchVisible(ctx, "ada", "tab-1", time.Minute); err != nil {
		t.Fatalf("TouchVisible: %v", err)
	}
	if err := tracker.TouchVisible(ctx, "ada", "tab-2", time.Minute); err != nil {
		t.Fatalf("TouchVisible: %v", err)
	}

	visible, err := tracker.Visible(ctx, []string{"ada", "bob"})
	if err != nil || !visible["ada"] || visible["bob"] {
		t.Fatalf("Visible = %v, %v; want only ada", visible, err)
	}

	if err := tracker.ClearVisible(ctx, "ada", "tab-1"); err != nil {
		t.Fatalf("ClearVisible: %v", err)
	}
	if visible, _ := tracker.Visible(ctx, []string{"ada"}); !visible["ada"] {
		t.Fatal("ada stopped being visible while another tab still shows the app")
	}

	now = now.Add(2 * time.Minute)
	if visible, _ := tracker.Visible(ctx, []string{"ada"}); visible["ada"] {
		t.Fatal("a tab that stopped reporting still counts as visible")
	}
}
