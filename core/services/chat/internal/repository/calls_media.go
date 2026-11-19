package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func (r *Repository) MediaEndpoint(ctx context.Context, binding calls.MediaBinding, now time.Time) (calls.Endpoint, error) {
	var endpoint calls.Endpoint
	var document []byte
	query := `SELECT e.id,e.user_id,e.family_id,e.epoch,e.lease_expires_at,c.document
 FROM call_endpoints e JOIN call_participants p ON p.endpoint_id=e.id
 JOIN calls c ON c.id=p.call_id
 LEFT JOIN room_members m ON m.room_id=c.room_id AND m.user_id=e.user_id
 JOIN call_user_slots s ON s.user_id=e.user_id AND s.call_id=c.id AND s.phase='busy'
 WHERE c.id=$1 AND e.id=$2 AND e.epoch=$3 AND e.lease_expires_at>$4 AND p.user_id=e.user_id AND (c.room_id IS NULL OR m.user_id IS NOT NULL)`
	if r.sfu {
		query += " AND e.supports_sfu"
	}
	err := r.q.QueryRow(ctx, query, binding.CallID, binding.EndpointID, binding.Epoch, now).Scan(&endpoint.ID, &endpoint.UserID, &endpoint.FamilyID, &endpoint.Epoch, &endpoint.LeaseExpiresAt, &document)
	if noRows(err) {
		return endpoint, calls.ErrForbidden
	}
	if err != nil {
		return endpoint, err
	}
	var call calls.Call
	if err := json.Unmarshal(document, &call); err != nil {
		return endpoint, err
	}
	participant := call.Participant(endpoint.UserID)
	if r.migration && call.Migration != nil && binding.Generation == call.Migration.TargetGeneration && call.TransportKind() == "p2p" {
		_, candidate, err := call.MigrationCandidate(endpoint.UserID)
		if err != nil || binding.MediaVersion != candidate.MediaVersion || !now.Before(call.Migration.Deadline) {
			return endpoint, calls.ErrForbidden
		}
		var supported bool
		if err = r.q.QueryRow(ctx, `SELECT supports_migration FROM call_endpoints WHERE id=$1`, endpoint.ID).Scan(&supported); err != nil {
			return endpoint, err
		}
		if !supported {
			return endpoint, calls.ErrForbidden
		}
		return endpoint, nil
	}
	if call.TransportKind() == "sfu" {
		if call.TransportPhase == "waiting_cleanup" || participant == nil || participant.SourcePending || !participant.SupportsSFU || binding.MediaVersion < 1 || participant.MediaVersion != binding.MediaVersion {
			return endpoint, calls.ErrForbidden
		}
	} else if !binding.AllowP2PProbe || binding.MediaVersion != 0 {
		return endpoint, calls.ErrForbidden
	}
	if (call.State != calls.Connecting && call.State != calls.Active) || call.Generation != binding.Generation || participant == nil || participant.EndpointID != endpoint.ID || participant.Epoch != endpoint.Epoch || participant.Status != "accepted" {
		return endpoint, calls.ErrForbidden
	}
	if !participant.Ready && !participant.MediaDeadline.IsZero() && !now.Before(participant.MediaDeadline) {
		return endpoint, calls.ErrForbidden
	}
	if call.State == calls.Connecting {
		deadline := call.ConnectionDeadline
		if call.TransportKind() == "p2p" {
			deadline = call.Deadline
		}
		if !deadline.IsZero() && !now.Before(deadline) {
			return endpoint, calls.ErrForbidden
		}
	}
	return endpoint, nil
}
