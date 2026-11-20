package calls

import (
	"time"

	"github.com/google/uuid"
)

const MaxParticipants = 6
const SFUSetupTimeout = 12 * time.Second

func (c Call) TransportKind() string {
	if c.Transport == "sfu" {
		return "sfu"
	}
	return "p2p"
}
func (c Call) Group() bool          { return c.Kind == "group" }
func (p Participant) Retired() bool { return p.Status != "accepted" && p.Status != "invited" }
func (c Call) ReadyCount() int {
	n := 0
	for _, p := range c.Participants {
		if p.Status == "accepted" && p.Ready {
			n++
		}
	}
	return n
}
func NewGroup(id, room, caller, key uuid.UUID, mode string, users []uuid.UUID, now time.Time) Call {
	c := Call{ID: id, RoomID: room, CallerID: caller, OwnerID: caller, StartKey: key, Mode: mode, Kind: "group", Transport: "sfu", Generation: 1, NegotiationID: uuid.New(), CreatedAt: now}
	for _, user := range users {
		c.Participants = append(c.Participants, Participant{UserID: user, Status: "invited", InviteUntil: now.Add(RingTimeout)})
	}
	return c
}
func (c *Call) groupDeadline() {
	if c.State == Ended {
		return
	}
	var next time.Time
	take := func(t time.Time) {
		if !t.IsZero() && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	if c.State == Connecting {
		take(c.ConnectionDeadline)
	}
	for _, p := range c.Participants {
		if p.Status == "invited" {
			take(p.InviteUntil)
		}
		if p.Status == "accepted" && !p.Ready {
			take(p.MediaDeadline)
		}
		if p.Retired() && !p.CleanupDone && !p.CleanupExpired {
			take(p.CleanupAt)
		}
	}
	c.Deadline = next
}
func (c *Call) rebalance(now time.Time) {
	if c.ReadyCount() >= 2 {
		c.State = Active
		c.EverActive = true
		c.Deadline = time.Time{}
		c.ConnectionDeadline = time.Time{}
		if c.StartedAt == nil {
			c.StartedAt = &now
		}
	} else {
		eligible := 0
		accepted := 0
		for _, p := range c.Participants {
			if p.Status == "accepted" {
				eligible++
				accepted++
			}
			if p.Status == "invited" && now.Before(p.InviteUntil) {
				eligible++
			}
		}
		if eligible < 2 {
			c.finish(now, "participants_left")
			return
		}
		if c.State == Active {
			c.State = Connecting
			c.ConnectionDeadline = now.Add(SFUSetupTimeout)
		}
		if c.State == Ringing && accepted >= 2 {
			c.State = Connecting
			c.ConnectionDeadline = now.Add(SFUSetupTimeout)
		}
	}
	c.groupDeadline()
}
func retire(c *Call, p *Participant, reason string, now time.Time) {
	p.RetiredVersion = c.Version + 1
	p.Status = reason
	p.LeftAt = &now
	p.Ready = false
	p.InviteUntil = time.Time{}
	p.MediaDeadline = time.Time{}
	p.CleanupAt = now.Add(CleanupTimeout)
	p.ReleaseAt = now.Add(Lease)
	if p.EndpointID == uuid.Nil && !p.SourcePending {
		p.CleanupDone = true
	}
	if c.OwnerID == p.UserID {
		c.OwnerID = uuid.Nil
		for _, other := range c.Participants {
			if other.UserID != p.UserID && other.Status == "accepted" {
				c.OwnerID = other.UserID
				break
			}
		}
	}
}
func RevokeParticipant(c *Call, user uuid.UUID, reason string, now time.Time) bool {
	p := c.Participant(user)
	if p == nil || p.Retired() || c.State == Ended {
		return false
	}
	retire(c, p, reason, now)
	c.rebalance(now)
	c.Version++
	return true
}
func ApplySFU(c *Call, actor uuid.UUID, cmd Command, now time.Time) (bool, error) {
	p := c.Participant(actor)
	if cmd.Action != "timeout" && p == nil {
		return false, ErrForbidden
	}
	if p != nil && (cmd.Action == "ready" || cmd.Action == "reconnecting" || cmd.Action == "cleanup") && cmd.MediaVersion != p.MediaVersion {
		return false, ErrStale
	}
	if c.State == Ended {
		switch cmd.Action {
		case "cleanup":
			if p.EndpointID != cmd.Endpoint.ID || p.Epoch != cmd.Endpoint.Epoch || cmd.Generation != c.Generation {
				return false, ErrForbidden
			}
			if p.CleanupDone {
				return false, nil
			}
			p.CleanupDone = true
		case "resume":
			if p.EndpointID != cmd.Endpoint.ID || p.Epoch >= cmd.Endpoint.Epoch {
				return false, ErrStale
			}
			p.Epoch = cmd.Endpoint.Epoch
		case "timeout":
			if c.Clean() {
				return false, nil
			}
			if now.Before(c.ReleaseAt) {
				c.Deadline = c.ReleaseAt
			} else {
				for i := range c.Participants {
					if !c.Participants[i].CleanupDone {
						c.Participants[i].CleanupExpired = true
					}
					c.Participants[i].SourcePending = false
				}
				c.Deadline = time.Time{}
			}
		default:
			return false, nil
		}
		if c.Clean() {
			c.Deadline = time.Time{}
		}
		c.Version++
		return true, nil
	}
	if cmd.Action != "timeout" && cmd.Action != "accept" && cmd.Action != "decline" && cmd.Action != "start_group" && cmd.Action != "resume" && (p.EndpointID != cmd.Endpoint.ID || p.Epoch != cmd.Endpoint.Epoch) {
		return false, ErrForbidden
	}
	if c.TransportPhase == "waiting_cleanup" {
		switch cmd.Action {
		case "cleanup":
			if c.PredecessorID != uuid.Nil || cmd.Generation != c.CleanupGeneration || !now.Before(c.Deadline) {
				return false, ErrConflict
			}
			if p.CleanupDone {
				return false, nil
			}
			p.CleanupDone = true
			if c.Clean() {
				c.TransportPhase = ""
				c.CleanupGeneration = 0
				c.Deadline = now.Add(SFUSetupTimeout)
				c.ConnectionDeadline = c.Deadline
				for i := range c.Participants {
					c.Participants[i].CleanupDone = false
					c.Participants[i].MediaVersion++
					c.Participants[i].MediaDeadline = c.Deadline
				}
			}
		case "end", "cancel":
			if c.Group() && actor != c.OwnerID {
				return false, ErrForbidden
			}
			c.finish(now, "cancelled")
		case "timeout":
			c.finish(now, "cleanup_timeout")
		default:
			return false, ErrConflict
		}
		c.Version++
		return true, nil
	}
	switch cmd.Action {
	case "start_group":
		if c.State != "" || actor != c.CallerID || !cmd.SFUCapable || len(c.Participants) < 2 || len(c.Participants) > MaxParticipants || (c.Mode != "audio" && c.Mode != "video") {
			return false, ErrInvalid
		}
		p.Status = "accepted"
		p.EndpointID = cmd.Endpoint.ID
		p.Epoch = cmd.Endpoint.Epoch
		p.SupportsSFU = true
		p.MediaVersion = 1
		p.InviteUntil = time.Time{}
		c.State = Ringing
		c.Deadline = now.Add(RingTimeout)
	case "accept":
		if p.Status != "invited" || !now.Before(p.InviteUntil) || !cmd.SFUCapable {
			return false, ErrConflict
		}
		p.Status = "accepted"
		p.EndpointID = cmd.Endpoint.ID
		p.Epoch = cmd.Endpoint.Epoch
		p.SupportsSFU = true
		p.MediaVersion++
		p.InviteUntil = time.Time{}
		p.MediaDeadline = now.Add(SFUSetupTimeout)
		if c.OwnerID == uuid.Nil {
			c.OwnerID = actor
		}
		if c.State == Ringing {
			c.State = Connecting
			c.Deadline = now.Add(SFUSetupTimeout)
			c.ConnectionDeadline = c.Deadline
			for i := range c.Participants {
				if c.Participants[i].Status == "accepted" && !c.Participants[i].Ready {
					c.Participants[i].MediaDeadline = c.Deadline
				}
			}
		}
		c.groupDeadline()
	case "decline":
		if p.Status != "invited" {
			return false, ErrConflict
		}
		retire(c, p, "declined", now)
		c.rebalance(now)
	case "cancel":
		if actor != c.OwnerID || c.State != Ringing {
			return false, ErrForbidden
		}
		c.finish(now, "cancelled")
	case "end":
		if c.Group() && actor != c.OwnerID {
			return false, ErrForbidden
		}
		c.finish(now, "ended")
	case "leave":
		if p.Status != "accepted" {
			return false, ErrConflict
		}
		retire(c, p, "left", now)
		c.rebalance(now)
	case "remove":
		if actor != c.OwnerID || p.Status != "accepted" || cmd.TargetID == actor {
			return false, ErrForbidden
		}
		target := c.Participant(cmd.TargetID)
		if target == nil || target.Retired() {
			return false, ErrConflict
		}
		retire(c, target, "removed", now)
		c.rebalance(now)
	case "invite":
		if actor != c.OwnerID || p.Status != "accepted" || cmd.TargetID == uuid.Nil {
			return false, ErrForbidden
		}
		target := c.Participant(cmd.TargetID)
		if target != nil {
			if !target.Retired() {
				return false, nil
			}
			if !target.CleanupDone && !target.CleanupExpired || target.SourcePending {
				return false, ErrConflict
			}
			version := target.MediaVersion
			*target = Participant{UserID: cmd.TargetID, Status: "invited", MediaVersion: version, InviteUntil: now.Add(RingTimeout)}
		} else {
			if len(c.Participants) >= MaxParticipants {
				return false, ErrBusy
			}
			c.Participants = append(c.Participants, Participant{UserID: cmd.TargetID, Status: "invited", InviteUntil: now.Add(RingTimeout)})
		}
		c.groupDeadline()
	case "reconnecting":
		if cmd.Generation != c.Generation || p.Status != "accepted" || !c.EverActive {
			return false, ErrStale
		}
		if !p.Ready {
			return false, nil
		}
		p.Ready = false
		p.MediaDeadline = now.Add(ReconnectTimeout)
		if c.ReadyCount() < 2 {
			c.State = Connecting
			c.ConnectionDeadline = p.MediaDeadline
		}
		c.groupDeadline()
	case "media_failed":
		if c.EverActive {
			return false, ErrConflict
		}
		c.finish(now, "media_unavailable")
	case "ready":
		if cmd.Generation != c.Generation || p.Status != "accepted" {
			return false, ErrStale
		}
		if !p.MediaDeadline.IsZero() && !now.Before(p.MediaDeadline) {
			return false, ErrConflict
		}
		if c.State == Connecting && !c.ConnectionDeadline.IsZero() && !now.Before(c.ConnectionDeadline) {
			return false, ErrConflict
		}
		if !cmd.Ready {
			retire(c, p, "connection_failed", now)
			c.rebalance(now)
			break
		}
		if p.Ready {
			return false, nil
		}
		p.Ready = true
		p.MediaDeadline = time.Time{}
		if p.ConnectedAt == nil {
			p.ConnectedAt = &now
		}
		c.rebalance(now)
	case "cleanup":
		if !p.Retired() || cmd.Generation != c.Generation {
			return false, ErrConflict
		}
		if p.CleanupDone {
			return false, nil
		}
		p.CleanupDone = true
		c.groupDeadline()
	case "resume":
		if p.EndpointID != cmd.Endpoint.ID || p.Epoch >= cmd.Endpoint.Epoch {
			return false, ErrStale
		}
		p.Epoch = cmd.Endpoint.Epoch
		p.MediaVersion++
		p.Ready = false
		p.SupportsSFU = cmd.SFUCapable
		if !cmd.SFUCapable {
			retire(c, p, "unsupported_endpoint", now)
			c.rebalance(now)
			break
		}
		p.MediaDeadline = now.Add(ReconnectTimeout)
		if c.ReadyCount() < 2 {
			c.State = Connecting
			c.ConnectionDeadline = p.MediaDeadline
		}
		c.groupDeadline()
	case "timeout":
		changed := false
		if cmd.Reason == "endpoint_expired" {
			for i := range c.Participants {
				member := &c.Participants[i]
				if member.EndpointExpired && !member.CleanupExpired {
					if !member.Retired() {
						retire(c, member, "endpoint_expired", now)
					}
					member.CleanupExpired = true
					member.SourcePending = false
					changed = true
				}
			}
		} else if c.State == Connecting && !c.ConnectionDeadline.IsZero() && !now.Before(c.ConnectionDeadline) {
			reason := "setup_timeout"
			if c.EverActive {
				reason = "reconnect_timeout"
			}
			c.finish(now, reason)
			changed = true
		} else {
			for i := range c.Participants {
				member := &c.Participants[i]
				if member.Status == "invited" && !now.Before(member.InviteUntil) {
					retire(c, member, "missed", now)
					changed = true
				}
				if member.Status == "accepted" && !member.Ready && !member.MediaDeadline.IsZero() && !now.Before(member.MediaDeadline) {
					retire(c, member, "connection_failed", now)
					changed = true
				}
				if member.Retired() && !member.CleanupDone && !member.CleanupExpired && !member.CleanupAt.IsZero() && !now.Before(member.CleanupAt) {
					if now.Before(member.ReleaseAt) {
						member.CleanupAt = member.ReleaseAt
					} else {
						member.CleanupExpired = true
						member.SourcePending = false
					}
					changed = true
				}
			}
		}
		if !changed {
			return false, nil
		}
		if c.State != Ended {
			c.rebalance(now)
		}
	default:
		return false, ErrInvalid
	}
	if c.State == Ended && c.Clean() {
		c.Deadline = time.Time{}
	}
	c.Version++
	return true, nil
}
