package repository

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func (r *Repository) ExpireCallFamily(ctx context.Context, user, family uuid.UUID, now time.Time) error {
	if !r.sfu {
		return nil
	}
	_, err := r.q.Exec(ctx, `UPDATE call_endpoints SET lease_expires_at=LEAST(lease_expires_at,$3) WHERE user_id=$1 AND family_id=$2`, user, family, now)
	return err
}

func (r *Repository) SetEndpointSFU(ctx context.Context, actor calls.Actor, ref calls.EndpointRef, supported bool, now time.Time) error {
	if !r.sfu {
		return calls.ErrUnavailable
	}
	return wrapCalls(r.InTx(ctx, func(tx *Repository) error {
		if _, err := tx.endpoint(ctx, actor, ref, now, true, true); err != nil {
			return err
		}
		_, err := tx.q.Exec(ctx, `UPDATE call_endpoints SET supports_sfu=$2 WHERE id=$1`, ref.ID, supported)
		return err
	}))
}
func (r *Repository) groupUsers(ctx context.Context, room, caller uuid.UUID, requested []uuid.UUID) ([]uuid.UUID, error) {
	if len(requested) > calls.MaxParticipants-1 {
		return nil, calls.ErrInvalid
	}
	var member bool
	if err := r.q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id=$1 AND user_id=$2)`, room, caller).Scan(&member); err != nil {
		return nil, err
	}
	if !member {
		return nil, calls.ErrForbidden
	}
	users := []uuid.UUID{caller}
	if len(requested) == 0 {
		rows, err := r.q.Query(ctx, `SELECT user_id FROM room_members WHERE room_id=$1 AND user_id<>$2 ORDER BY user_id LIMIT $3`, room, caller, calls.MaxParticipants)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var user uuid.UUID
			if err := rows.Scan(&user); err != nil {
				rows.Close()
				return nil, err
			}
			users = append(users, user)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	} else {
		seen := map[uuid.UUID]bool{caller: true}
		for _, user := range requested {
			if user == uuid.Nil || seen[user] {
				return nil, calls.ErrInvalid
			}
			seen[user] = true
			if err := r.q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id=$1 AND user_id=$2)`, room, user).Scan(&member); err != nil {
				return nil, err
			}
			if !member {
				return nil, calls.ErrForbidden
			}
			users = append(users, user)
		}
	}
	if len(users) < 2 || len(users) > calls.MaxParticipants {
		return nil, calls.ErrInvalid
	}
	return users, nil
}
func (r *Repository) invitable(ctx context.Context, call calls.Call, actor, target uuid.UUID) error {
	if target == uuid.Nil || actor == target {
		return calls.ErrInvalid
	}
	var allowed bool
	if call.RoomID != uuid.Nil {
		if err := r.q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id=$1 AND user_id=$2)`, call.RoomID, target).Scan(&allowed); err != nil {
			return err
		}
	} else {
		if err := r.q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM rooms r JOIN room_members a ON a.room_id=r.id AND a.user_id=$1 JOIN room_members b ON b.room_id=r.id AND b.user_id=$2 WHERE r.kind='direct' AND ((r.direct_user_min=$1 AND r.direct_user_max=$2) OR (r.direct_user_min=$2 AND r.direct_user_max=$1)))`, actor, target).Scan(&allowed); err != nil {
			return err
		}
	}
	if !allowed {
		return calls.ErrForbidden
	}
	return nil
}
func (r *Repository) ReplaceCall(ctx context.Context, actor calls.Actor, cmd calls.Command, now time.Time) (calls.Call, error) {
	var result calls.Call
	if !r.sfu {
		return result, calls.ErrUnavailable
	}
	err := r.InTx(ctx, func(tx *Repository) error {
		endpoint, err := tx.endpoint(ctx, actor, cmd.Endpoint, now, true, true)
		if err != nil {
			return err
		}
		if !endpoint.SupportsSFU {
			return calls.ErrForbidden
		}
		fingerprint, err := callFingerprint(cmd)
		if err != nil {
			return err
		}
		if _, err := tx.q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "calls:request:"+actor.UserID.String()+":"+cmd.RequestID.String()); err != nil {
			return err
		}
		var previousHash, previous []byte
		err = tx.q.QueryRow(ctx, `SELECT fingerprint,result FROM call_requests WHERE user_id=$1 AND request_id=$2`, actor.UserID, cmd.RequestID).Scan(&previousHash, &previous)
		if err == nil {
			if subtle.ConstantTimeCompare(previousHash, fingerprint[:]) != 1 {
				return calls.ErrConflict
			}
			return json.Unmarshal(previous, &result)
		}
		if !noRows(err) {
			return err
		}
		var room *uuid.UUID
		if err := tx.q.QueryRow(ctx, `SELECT room_id FROM calls WHERE id=$1`, cmd.ID).Scan(&room); err != nil {
			return err
		}
		if room != nil {
			if err := tx.lockRoom(ctx, *room); err != nil {
				return err
			}
		}
		var raw []byte
		if err := tx.q.QueryRow(ctx, `SELECT document FROM calls WHERE id=$1 FOR UPDATE`, cmd.ID).Scan(&raw); err != nil {
			return err
		}
		var source calls.Call
		if err := json.Unmarshal(raw, &source); err != nil {
			return err
		}
		own := source.Participant(actor.UserID)
		if source.State != calls.Active || source.Group() || source.ReplacedByID != uuid.Nil || len(source.Participants) != 2 || own == nil || own.EndpointID != cmd.Endpoint.ID || own.Epoch != cmd.Endpoint.Epoch {
			return calls.ErrConflict
		}
		if source.Participant(cmd.TargetID) != nil {
			return calls.ErrInvalid
		}
		eligibility := calls.Call{}
		if err := tx.invitable(ctx, eligibility, actor.UserID, cmd.TargetID); err != nil {
			return err
		}
		users := make([]uuid.UUID, 0, 3)
		for _, p := range source.Participants {
			var capable bool
			if err := tx.q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM call_endpoints WHERE id=$1 AND epoch=$2 AND supports_sfu AND lease_expires_at>$3)`, p.EndpointID, p.Epoch, now).Scan(&capable); err != nil {
				return err
			}
			if !capable {
				return calls.ErrForbidden
			}
			users = append(users, p.UserID)
			if room != nil {
				var member bool
				if err := tx.q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id=$1 AND user_id=$2)`, *room, p.UserID).Scan(&member); err != nil {
					return err
				}
				if !member {
					return calls.ErrForbidden
				}
			}
		}
		users = append(users, cmd.TargetID)
		sort.Slice(users, func(i, j int) bool { return users[i].String() < users[j].String() })
		for _, user := range users {
			if _, err := tx.q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "calls:slot:"+user.String()); err != nil {
				return err
			}
			var occupied uuid.UUID
			err := tx.q.QueryRow(ctx, `SELECT call_id FROM call_user_slots WHERE user_id=$1`, user).Scan(&occupied)
			if user == cmd.TargetID {
				if err == nil {
					return calls.ErrBusy
				}
				if !noRows(err) {
					return err
				}
			} else {
				if err != nil {
					return err
				}
				if occupied != source.ID {
					return calls.ErrBusy
				}
			}
		}
		result = calls.NewGroup(uuid.New(), uuid.Nil, actor.UserID, cmd.RequestID, source.Mode, users, now)
		result.PredecessorID = source.ID
		result.PredecessorViewers = []uuid.UUID{source.Participants[0].UserID, source.Participants[1].UserID}
		result.State = calls.Ringing
		result.TransportPhase = "waiting_cleanup"
		result.Deadline = now.Add(calls.CleanupTimeout)
		result.Version = 1
		for i := range result.Participants {
			p := &result.Participants[i]
			p.InviteUntil = time.Time{}
			if source.Participant(p.UserID) != nil {
				p.SourcePending = true
			}
			if p.UserID == actor.UserID {
				p.Status = "accepted"
				p.EndpointID = cmd.Endpoint.ID
				p.Epoch = cmd.Endpoint.Epoch
				p.SupportsSFU = true
				p.MediaVersion = 1
			}
		}
		body, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if err := tx.writeCall(ctx, result, body); err != nil {
			return err
		}
		if _, err := tx.q.Exec(ctx, `UPDATE call_user_slots SET call_id=$2 WHERE call_id=$1`, source.ID, result.ID); err != nil {
			return err
		}
		source.ReplacedByID = result.ID
		changed, err := calls.Apply(&source, actor.UserID, calls.Command{Action: "end", Endpoint: cmd.Endpoint}, now)
		if err != nil {
			return err
		}
		if !changed {
			return calls.ErrConflict
		}
		source.EndReason = "replaced_by_group"
		if err := tx.persistCall(ctx, source, now); err != nil {
			return err
		}
		if err := tx.persistCall(ctx, result, now); err != nil {
			return err
		}
		_, err = tx.q.Exec(ctx, `INSERT INTO call_requests(user_id,request_id,fingerprint,result,call_id) VALUES($1,$2,$3,$4,$5)`, actor.UserID, cmd.RequestID, fingerprint[:], body, result.ID)
		return err
	})
	return result, wrapCalls(err)
}
func (r *Repository) advanceReplacement(ctx context.Context, source calls.Call, now time.Time) error {
	var raw []byte
	if err := r.q.QueryRow(ctx, `SELECT document FROM calls WHERE id=$1 FOR UPDATE`, source.ReplacedByID).Scan(&raw); err != nil {
		return err
	}
	var target calls.Call
	if err := json.Unmarshal(raw, &target); err != nil {
		return err
	}
	changed := false
	for _, old := range source.Participants {
		p := target.Participant(old.UserID)
		if p != nil && p.SourcePending && old.CleanupDone {
			p.SourcePending = false
			changed = true
			if target.State == calls.Ended && p.EndpointID == uuid.Nil {
				p.CleanupDone = true
			}
		}
	}
	if !changed {
		return nil
	}
	if target.State != calls.Ended && target.TransportPhase == "waiting_cleanup" {
		all := true
		for _, p := range target.Participants {
			all = all && !p.SourcePending
		}
		if all && now.Before(target.Deadline) {
			target.TransportPhase = ""
			target.Deadline = now.Add(calls.RingTimeout)
			for i := range target.Participants {
				if target.Participants[i].Status == "invited" {
					target.Participants[i].InviteUntil = target.Deadline
				}
			}
		}
	}
	target.Version++
	if target.State == calls.Ended && target.Clean() {
		target.Deadline = time.Time{}
	}
	return r.persistCall(ctx, target, now)
}
func (r *Repository) revokeRoomCalls(ctx context.Context, room, user uuid.UUID) error {
	rows, err := r.q.Query(ctx, `SELECT document FROM calls WHERE room_id=$1 AND state<>'ended' AND document->>'kind'='group' ORDER BY id FOR UPDATE`, room)
	if err != nil {
		return err
	}
	var active []calls.Call
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var call calls.Call
		if err := json.Unmarshal(raw, &call); err != nil {
			rows.Close()
			return err
		}
		active = append(active, call)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, call := range active {
		if calls.RevokeParticipant(&call, user, "removed", now) {
			if err := r.persistCall(ctx, call, now); err != nil {
				return err
			}
		}
	}
	return nil
}
