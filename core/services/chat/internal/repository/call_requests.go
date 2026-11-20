package repository

import (
	"crypto/sha256"
	"encoding/json"
	"sort"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func callFingerprint(cmd calls.Command) ([32]byte, error) {
	legacy := struct {
		Action                string
		ID, RoomID, RequestID uuid.UUID
		Endpoint              calls.EndpointRef
		Mode                  string
		Generation            int64
		Ready                 bool
		Reason                string
	}{cmd.Action, cmd.ID, cmd.RoomID, cmd.RequestID, calls.EndpointRef{ID: cmd.Endpoint.ID}, cmd.Mode, cmd.Generation, cmd.Ready, cmd.Reason}
	if cmd.StartsDirect() || cmd.Action == "start_group" {
		legacy.ID = uuid.Nil
	}
	var input any = legacy
	if cmd.Action == "start_direct" || cmd.Action == "start_group" || cmd.Action == "replace" || cmd.Action == "invite" || cmd.Action == "remove" || cmd.Action == "leave" {
		users := append([]uuid.UUID(nil), cmd.Users...)
		sort.Slice(users, func(i, j int) bool { return users[i].String() < users[j].String() })
		input = struct {
			Legacy   any
			Users    []uuid.UUID
			TargetID uuid.UUID
		}{legacy, users, cmd.TargetID}
	}
	if cmd.MediaVersion != 0 {
		input = struct {
			Command      any
			MediaVersion int64
		}{input, cmd.MediaVersion}
	}
	if cmd.MigrationID != uuid.Nil {
		input = struct {
			Command     any
			MigrationID uuid.UUID
		}{input, cmd.MigrationID}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}
