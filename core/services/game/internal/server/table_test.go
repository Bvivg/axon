package server

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

func TestTableOfMapsDurakOptions(t *testing.T) {
	options := &gamev1.GameOptions{Game: &gamev1.GameOptions_Durak{Durak: &gamev1.DurakOptions{
		Mode: gamev1.DurakMode_DURAK_MODE_TRANSFER, Cheats: true, Deck: 52, ThrowIn: gamev1.DurakThrowIn_DURAK_THROW_IN_NEIGHBORS, Draw: true, FirstBoutLimit: 5,
	}}}
	table, err := tableOf(durak.Key, proto.Uint32(5), proto.Uint32(3), options)
	if err != nil || table.Seats != 5 || table.Bots != 3 {
		t.Fatal(table, err)
	}
	var got durak.Options
	if err := json.Unmarshal(table.Options, &got); err != nil {
		t.Fatal(err)
	}
	want := durak.Options{Mode: durak.ModeTransfer, Cheats: true, Deck: 52, ThrowIn: durak.ThrowInNeighbors, Draw: true, FirstBoutLimit: 5, Seats: 5}
	if got != want {
		t.Fatalf("options %+v, want %+v", got, want)
	}
	if _, err := durak.ParseOptions(table.Options, 5); err != nil {
		t.Fatal("the engine rejects mapped options", err)
	}
	back := optionsProto(durak.Key, table.Options).GetDurak()
	if !proto.Equal(back, options.GetDurak()) {
		t.Fatalf("round trip %v", back)
	}
	if !cheating(durak.Key, table.Options) {
		t.Fatal("cheating table not detected")
	}
}

func TestTableOfDefaultsAndRejections(t *testing.T) {
	plain, err := tableOf(chess.Key, proto.Uint32(2), nil, nil)
	if err != nil || !plain.Plain() {
		t.Fatal("two-seat chess must stay a plain game", plain, err)
	}
	defaults, err := tableOf(durak.Key, proto.Uint32(4), nil, nil)
	if err != nil || len(defaults.Options) == 0 {
		t.Fatal("a four-seat durak table needs default options", err)
	}
	if _, err := durak.ParseOptions(defaults.Options, 4); err != nil {
		t.Fatal(err)
	}
	durakOptions := &gamev1.GameOptions{Game: &gamev1.GameOptions_Durak{Durak: &gamev1.DurakOptions{}}}
	for name, call := range map[string]func() error{
		"one seat":            func() error { _, err := tableOf(durak.Key, proto.Uint32(1), nil, nil); return err },
		"seven seats":         func() error { _, err := tableOf(durak.Key, proto.Uint32(7), nil, nil); return err },
		"durak options chess": func() error { _, err := tableOf(chess.Key, nil, nil, durakOptions); return err },
		"unknown mode": func() error {
			_, err := tableOf(durak.Key, nil, nil, &gamev1.GameOptions{Game: &gamev1.GameOptions_Durak{Durak: &gamev1.DurakOptions{Mode: 9}}})
			return err
		},
	} {
		if call() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
