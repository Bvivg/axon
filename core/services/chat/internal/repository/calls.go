package repository

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

var _ calls.Store = (*Repository)(nil)

func (r *Repository) RegisterEndpoint(ctx context.Context, actor calls.Actor, resume *calls.EndpointRef, proof string, now time.Time) (calls.Endpoint, error) {
	var result calls.Endpoint
	err := r.InTx(ctx, func(tx *Repository) error {
		if _, err := tx.q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "calls:endpoints:"+actor.UserID.String()); err != nil {
			return err
		}
		if resume != nil {
			var current int64
			if err := tx.q.QueryRow(ctx, `SELECT epoch FROM call_endpoints WHERE id=$1`, resume.ID).Scan(&current); err != nil {
				if noRows(err) {
					return calls.ErrForbidden
				}
				return err
			}
			if resume.Epoch > current {
				return calls.ErrStale
			}
			reference := *resume
			reference.Epoch = current
			endpoint, err := tx.endpoint(ctx, actor, reference, now, false, false)
			if err != nil {
				return err
			}
			endpoint.Epoch++
			endpoint.LeaseExpiresAt = now.Add(calls.Lease)
			if _, err = tx.q.Exec(ctx, `UPDATE call_endpoints SET epoch=$2,lease_expires_at=$3,connection_id=NULL WHERE id=$1`, endpoint.ID, endpoint.Epoch, endpoint.LeaseExpiresAt); err != nil {
				return err
			}
			result = endpoint
			return nil
		}
		var count int
		if err := tx.q.QueryRow(ctx, `SELECT count(*) FROM call_endpoints WHERE user_id=$1 AND lease_expires_at>$2`, actor.UserID, now).Scan(&count); err != nil {
			return err
		}
		if count >= 8 {
			return calls.ErrBusy
		}
		hash := sha256.Sum256([]byte(proof))
		result = calls.Endpoint{ID: uuid.New(), UserID: actor.UserID, FamilyID: actor.FamilyID, Epoch: 1, LeaseExpiresAt: now.Add(calls.Lease)}
		_, err := tx.q.Exec(ctx, `INSERT INTO call_endpoints(id,user_id,family_id,proof_hash,epoch,lease_expires_at) VALUES($1,$2,$3,$4,$5,$6)`, result.ID, result.UserID, result.FamilyID, hash[:], result.Epoch, result.LeaseExpiresAt)
		return err
	})
	return result, wrapCalls(err)
}

func (r *Repository) endpoint(ctx context.Context, actor calls.Actor, ref calls.EndpointRef, now time.Time, live, lock bool) (calls.Endpoint, error) {
	var endpoint calls.Endpoint
	var hash []byte
	columns := "id,user_id,family_id,proof_hash,epoch,lease_expires_at"
	if r.sfu {
		columns += ",supports_sfu"
	}
	if r.migration {
		columns += ",supports_migration"
	}
	query := "SELECT " + columns + " FROM call_endpoints WHERE id=$1"
	if lock {
		query += ` FOR SHARE`
	}
	fields := []any{&endpoint.ID, &endpoint.UserID, &endpoint.FamilyID, &hash, &endpoint.Epoch, &endpoint.LeaseExpiresAt}
	if r.sfu {
		fields = append(fields, &endpoint.SupportsSFU)
	}
	if r.migration {
		fields = append(fields, &endpoint.SupportsMigration)
	}
	err := r.q.QueryRow(ctx, query, ref.ID).Scan(fields...)
	if noRows(err) {
		return endpoint, calls.ErrForbidden
	}
	if err != nil {
		return endpoint, err
	}
	expected := ref.Hash()
	if endpoint.UserID != actor.UserID || endpoint.FamilyID != actor.FamilyID || subtle.ConstantTimeCompare(hash, expected[:]) != 1 {
		return endpoint, calls.ErrForbidden
	}
	if endpoint.Epoch != ref.Epoch || (live && !now.Before(endpoint.LeaseExpiresAt)) {
		return endpoint, calls.ErrStale
	}
	return endpoint, nil
}
func (r *Repository) Endpoint(ctx context.Context, actor calls.Actor, ref calls.EndpointRef, now time.Time, renew bool) (calls.Endpoint, error) {
	var endpoint calls.Endpoint
	err := r.InTx(ctx, func(tx *Repository) error {
		var err error
		endpoint, err = tx.endpoint(ctx, actor, ref, now, true, !renew)
		if err != nil {
			return err
		}
		if renew {
			endpoint.LeaseExpiresAt = now.Add(calls.Lease)
			tag, err := tx.q.Exec(ctx, `UPDATE call_endpoints SET lease_expires_at=$2 WHERE id=$1 AND epoch=$3 AND lease_expires_at>$4`, ref.ID, endpoint.LeaseExpiresAt, ref.Epoch, now)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return calls.ErrStale
			}
		}
		return nil
	})
	return endpoint, wrapCalls(err)
}

func (r *Repository) Mutate(ctx context.Context, actor calls.Actor, cmd calls.Command, now time.Time, apply func(*calls.Call) (bool, error)) (calls.Call, error) {
	var result calls.Call
	starting := cmd.StartsDirect() || cmd.Action == "start_group"
	err := r.InTx(ctx, func(tx *Repository) error {
		if cmd.Action != "timeout" {
			if _, err := tx.endpoint(ctx, actor, cmd.Endpoint, now, true, true); err != nil {
				return err
			}
		}
		fingerprint, err := callFingerprint(cmd)
		if err != nil {
			return err
		}
		if cmd.RequestID != uuid.Nil {
			if _, err := tx.q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "calls:request:"+actor.UserID.String()+":"+cmd.RequestID.String()); err != nil {
				return err
			}
			var priorHash, prior []byte
			err := tx.q.QueryRow(ctx, `SELECT fingerprint,result FROM call_requests WHERE user_id=$1 AND request_id=$2`, actor.UserID, cmd.RequestID).Scan(&priorHash, &prior)
			if err == nil {
				if subtle.ConstantTimeCompare(priorHash, fingerprint[:]) != 1 {
					return calls.ErrConflict
				}
				return json.Unmarshal(prior, &result)
			}
			if !noRows(err) {
				return err
			}
		}
		roomID := cmd.RoomID
		if !starting {
			var nullableRoom *uuid.UUID
			if err := tx.q.QueryRow(ctx, `SELECT room_id FROM calls WHERE id=$1`, cmd.ID).Scan(&nullableRoom); err != nil {
				if noRows(err) {
					return calls.ErrNotFound
				}
				return err
			}
			if nullableRoom != nil {
				roomID = *nullableRoom
			}
		}
		var kind string
		var first, second *uuid.UUID
		if roomID != uuid.Nil {
			if err := tx.q.QueryRow(ctx, `SELECT kind,direct_user_min,direct_user_max FROM rooms WHERE id=$1 FOR NO KEY UPDATE`, roomID).Scan(&kind, &first, &second); err != nil {
				if noRows(err) {
					return calls.ErrNotFound
				}
				return err
			}
		}
		switch cmd.Action {
		case "start_direct":
			if roomID != uuid.Nil || cmd.TargetID == uuid.Nil || cmd.TargetID == actor.UserID {
				return calls.ErrInvalid
			}
			result = calls.New(cmd.ID, uuid.Nil, actor.UserID, cmd.TargetID, cmd.RequestID, cmd.Mode, now)
		case "start_group":
			if !tx.sfu || kind != "group" {
				return calls.ErrForbidden
			}
			users, err := tx.groupUsers(ctx, roomID, actor.UserID, cmd.Users)
			if err != nil {
				return err
			}
			result = calls.NewGroup(cmd.ID, roomID, actor.UserID, cmd.RequestID, cmd.Mode, users, now)
		case "start":
			if kind != "direct" || first == nil || second == nil || (*first != actor.UserID && *second != actor.UserID) {
				return calls.ErrForbidden
			}
			var members int
			if err := tx.q.QueryRow(ctx, `SELECT count(*) FROM room_members WHERE room_id=$1 AND user_id IN ($2,$3)`, roomID, *first, *second).Scan(&members); err != nil {
				return err
			}
			if members != 2 {
				return calls.ErrForbidden
			}
			var prior []byte
			err := tx.q.QueryRow(ctx, `SELECT document FROM calls WHERE caller_id=$1 AND start_key=$2`, actor.UserID, cmd.RequestID).Scan(&prior)
			if err == nil {
				if err = json.Unmarshal(prior, &result); err != nil {
					return err
				}
				if result.RoomID != roomID || result.Mode != cmd.Mode {
					return calls.ErrConflict
				}
				return nil
			}
			if !noRows(err) {
				return err
			}
			peer := *first
			if peer == actor.UserID {
				peer = *second
			}
			result = calls.New(cmd.ID, roomID, actor.UserID, peer, cmd.RequestID, cmd.Mode, now)
		default:
			var raw []byte
			if err := tx.q.QueryRow(ctx, `SELECT document FROM calls WHERE id=$1 FOR UPDATE`, cmd.ID).Scan(&raw); err != nil {
				return err
			}
			if err = json.Unmarshal(raw, &result); err != nil {
				return err
			}
			if cmd.Action != "timeout" {
				if result.Participant(actor.UserID) == nil {
					return calls.ErrForbidden
				}
				if roomID != uuid.Nil && cmd.Action != "cleanup" {
					var member bool
					if err := tx.q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id=$1 AND user_id=$2)`, roomID, actor.UserID).Scan(&member); err != nil {
						return err
					}
					if !member {
						return calls.ErrForbidden
					}
				}
			}
		}
		if cmd.Action == "invite" {
			if !tx.sfu || !result.Group() {
				return calls.ErrForbidden
			}
			if err := tx.invitable(ctx, result, actor.UserID, cmd.TargetID); err != nil {
				return err
			}
		}
		if cmd.Action == "migrate" {
			if !tx.migration {
				return calls.ErrUnavailable
			}
			for _, p := range result.Participants {
				var epoch int64
				var until time.Time
				var sfu, migration bool
				if err := tx.q.QueryRow(ctx, `SELECT epoch,lease_expires_at,supports_sfu,supports_migration FROM call_endpoints WHERE id=$1 AND user_id=$2 FOR SHARE`, p.EndpointID, p.UserID).Scan(&epoch, &until, &sfu, &migration); err != nil {
					return err
				}
				if epoch != p.Epoch || !now.Before(until) {
					return calls.ErrStale
				}
				if !sfu || !migration {
					return calls.ErrForbidden
				}
			}
		}
		if cmd.Action == "timeout" && cmd.Reason == "endpoint_expired" {
			rows, err := tx.q.Query(ctx, `SELECT e.user_id FROM call_participants p JOIN call_endpoints e ON e.id=p.endpoint_id WHERE p.call_id=$1 AND e.lease_expires_at<=$2 FOR SHARE OF e`, result.ID, now)
			if err != nil {
				return err
			}
			expired := false
			for rows.Next() {
				var user uuid.UUID
				if err := rows.Scan(&user); err != nil {
					rows.Close()
					return err
				}
				if p := result.Participant(user); p != nil {
					p.EndpointExpired = true
					expired = true
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if !expired {
				return nil
			}
		}
		changed, err := apply(&result)
		if err != nil {
			return err
		}
		if changed {
			if err := tx.persistCall(ctx, result, now); err != nil {
				return err
			}
			if tx.sfu && result.ReplacedByID != uuid.Nil {
				if err := tx.advanceReplacement(ctx, result, now); err != nil {
					return err
				}
			}
		}
		if cmd.RequestID != uuid.Nil {
			document, err := json.Marshal(result)
			if err != nil {
				return err
			}
			_, err = tx.q.Exec(ctx, `INSERT INTO call_requests(user_id,request_id,fingerprint,result,call_id) VALUES($1,$2,$3,$4,$5)`, actor.UserID, cmd.RequestID, fingerprint[:], document, result.ID)
			return err
		}
		return nil
	})
	return result, wrapCalls(err)
}

func (r *Repository) GetCall(ctx context.Context, user, id uuid.UUID) (calls.Call, error) {
	var raw []byte
	err := r.q.QueryRow(ctx, `SELECT c.document FROM calls c JOIN call_participants p ON p.call_id=c.id WHERE c.id=$1 AND p.user_id=$2`, id, user).Scan(&raw)
	if noRows(err) {
		return calls.Call{}, calls.ErrNotFound
	}
	if err != nil {
		return calls.Call{}, wrapCalls(err)
	}
	var call calls.Call
	err = json.Unmarshal(raw, &call)
	return call, wrapCalls(err)
}
func (r *Repository) ActiveCall(ctx context.Context, user uuid.UUID) (*calls.Call, error) {
	var raw []byte
	err := r.q.QueryRow(ctx, `SELECT c.document FROM calls c JOIN call_user_slots s ON s.call_id=c.id WHERE s.user_id=$1`, user).Scan(&raw)
	if noRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapCalls(err)
	}
	var call calls.Call
	err = json.Unmarshal(raw, &call)
	return &call, wrapCalls(err)
}
func (r *Repository) ListCalls(ctx context.Context, user, before uuid.UUID, limit int) ([]calls.Call, error) {
	if limit < 1 || limit > 51 {
		limit = 25
	}
	rows, err := r.q.Query(ctx, `SELECT c.document FROM calls c JOIN call_participants p ON p.call_id=c.id WHERE p.user_id=$1 AND ($2::uuid IS NULL OR (c.created_at,c.id)<(SELECT created_at,id FROM calls WHERE id=$2 AND EXISTS(SELECT 1 FROM call_participants WHERE call_id=$2 AND user_id=$1))) ORDER BY c.created_at DESC,c.id DESC LIMIT $3`, user, nullCallID(before), limit)
	if err != nil {
		return nil, wrapCalls(err)
	}
	defer rows.Close()
	var result []calls.Call
	for rows.Next() {
		var raw []byte
		var call calls.Call
		if err := rows.Scan(&raw); err != nil {
			return nil, wrapCalls(err)
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, wrapCalls(err)
		}
		result = append(result, call)
	}
	return result, wrapCalls(rows.Err())
}
func nullCallTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
func nullCallID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
func wrapCalls(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return calls.ErrNotFound
	}
	return fmt.Errorf("repository calls: %w", err)
}
