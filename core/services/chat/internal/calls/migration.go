package calls

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

const MigrationPrepareTimeout = 8 * time.Second
const MigrationCommitTimeout = 3 * time.Second
const MigrationCleanupTimeout = 3 * time.Second

type MigrationPeer struct {
	UserID        uuid.UUID `json:"user_id"`
	EndpointID    uuid.UUID `json:"endpoint_id"`
	Epoch         int64     `json:"epoch"`
	MediaVersion  int64     `json:"media_version"`
	Ready         bool      `json:"ready"`
	Switched      bool      `json:"switched"`
	Clean         bool      `json:"clean"`
	AbortClean    bool      `json:"abort_clean"`
	SourceHealthy bool      `json:"source_healthy"`
}
type Migration struct {
	ID               uuid.UUID       `json:"id"`
	SourceGeneration int64           `json:"source_generation"`
	TargetGeneration int64           `json:"target_generation"`
	Phase            string          `json:"phase"`
	Outcome          string          `json:"outcome"`
	CreatedAt        time.Time       `json:"created_at"`
	Deadline         time.Time       `json:"deadline"`
	FinishedAt       *time.Time      `json:"finished_at,omitempty"`
	Peers            []MigrationPeer `json:"peers"`
}

func (m Migration) Open() bool {
	return m.Phase == "preparing" || m.Phase == "committing" || m.Phase == "cleaning" || m.Phase == "aborting"
}
func (m *Migration) Peer(user uuid.UUID) *MigrationPeer {
	for i := range m.Peers {
		if m.Peers[i].UserID == user {
			return &m.Peers[i]
		}
	}
	return nil
}
func (m Migration) all(test func(MigrationPeer) bool) bool {
	if len(m.Peers) != 2 {
		return false
	}
	for _, p := range m.Peers {
		if !test(p) {
			return false
		}
	}
	return true
}
func (c Call) MigrationCandidate(user uuid.UUID) (Call, Participant, error) {
	m := c.Migration
	p := c.Participant(user)
	if c.State != Active || c.TransportKind() != "p2p" || m == nil || (m.Phase != "preparing" && m.Phase != "committing") || p == nil {
		return Call{}, Participant{}, ErrConflict
	}
	peer := m.Peer(user)
	if peer == nil || p.Status != "accepted" || p.EndpointID != peer.EndpointID || p.Epoch != peer.Epoch || !p.SupportsMigration {
		return Call{}, Participant{}, ErrStale
	}
	candidate := c
	candidate.Generation = m.TargetGeneration
	candidate.Transport = "sfu"
	candidate.Migration = nil
	participant := *p
	participant.MediaVersion = peer.MediaVersion
	return candidate, participant, nil
}
func Apply(c *Call, actor uuid.UUID, cmd Command, now time.Time) (bool, error) {
	if cmd.Action == "migrate" || strings.HasPrefix(cmd.Action, "migration_") {
		return applyMigration(c, actor, cmd, now)
	}
	if m := c.Migration; m != nil && m.Open() {
		if m.Phase == "cleaning" && (cmd.Action == "ready" || cmd.Action == "reconnecting" || cmd.Action == "resume") {
			p := c.Participant(actor)
			if p == nil || p.EndpointID != cmd.Endpoint.ID || (cmd.Action != "resume" && (p.Epoch != cmd.Endpoint.Epoch || cmd.Generation != c.Generation || cmd.MediaVersion != p.MediaVersion)) {
				return false, ErrStale
			}
			if cmd.Action == "ready" && cmd.Ready {
				return false, nil
			}
			c.finish(now, "migration_connection_failed")
			c.Version++
			return true, nil
		}
		if cmd.Action == "timeout" && cmd.Reason == "" {
			if now.Before(m.Deadline) {
				return false, nil
			}
			switch m.Phase {
			case "preparing", "committing":
				beginAbort(c, "migration_timeout", now)
			case "aborting", "cleaning":
				c.finish(now, "migration_cleanup_timeout")
			}
			c.Version++
			return true, nil
		}
		if cmd.Action == "ready" && c.TransportKind() == "p2p" {
			p := c.Participant(actor)
			if p == nil || p.EndpointID != cmd.Endpoint.ID || p.Epoch != cmd.Endpoint.Epoch || cmd.Generation != c.Generation {
				return false, ErrStale
			}
			return false, nil
		}
		if cmd.Action == "resume" && c.TransportKind() == "p2p" {
			p := c.Participant(actor)
			if p == nil || p.EndpointID != cmd.Endpoint.ID || p.Epoch >= cmd.Endpoint.Epoch {
				return false, ErrStale
			}
			m.Phase = "aborted"
			m.Outcome = "endpoint_resumed"
			m.FinishedAt = &now
		}
	}
	return applyBase(c, actor, cmd, now)
}
func beginAbort(c *Call, reason string, now time.Time) {
	c.Migration.Phase = "aborting"
	c.Migration.Outcome = reason
	c.Migration.Deadline = now.Add(MigrationCleanupTimeout)
	c.Deadline = c.Migration.Deadline
}
func applyMigration(c *Call, actor uuid.UUID, cmd Command, now time.Time) (bool, error) {
	p := c.Participant(actor)
	if p == nil || p.Status != "accepted" || p.EndpointID != cmd.Endpoint.ID || p.Epoch != cmd.Endpoint.Epoch {
		return false, ErrForbidden
	}
	if c.State == Ended {
		return false, ErrStale
	}
	if cmd.Action == "migrate" {
		if m := c.Migration; m != nil && m.Open() {
			if cmd.Generation != m.SourceGeneration {
				return false, ErrStale
			}
			return false, nil
		}
		if !cmd.EnableMigration || !cmd.EnableSFU || c.Group() || c.TransportKind() != "p2p" || c.State != Active || !c.EverActive || cmd.Generation != c.Generation || len(c.Participants) != 2 {
			return false, ErrConflict
		}
		for _, peer := range c.Participants {
			if peer.Status != "accepted" || !peer.SupportsSFU || !peer.SupportsMigration || !peer.Ready {
				return false, ErrForbidden
			}
		}
		if c.TransportSerial < c.Generation {
			c.TransportSerial = c.Generation
		}
		c.TransportSerial++
		m := &Migration{ID: uuid.New(), SourceGeneration: c.Generation, TargetGeneration: c.TransportSerial, Phase: "preparing", CreatedAt: now, Deadline: now.Add(MigrationPrepareTimeout)}
		for _, peer := range c.Participants {
			m.Peers = append(m.Peers, MigrationPeer{UserID: peer.UserID, EndpointID: peer.EndpointID, Epoch: peer.Epoch, MediaVersion: peer.MediaVersion + 1})
		}
		c.Migration = m
		c.Deadline = m.Deadline
		c.Version++
		return true, nil
	}
	m := c.Migration
	if m == nil || m.ID != cmd.MigrationID || m.TargetGeneration != cmd.Generation {
		return false, ErrStale
	}
	peer := m.Peer(actor)
	if peer == nil || peer.EndpointID != cmd.Endpoint.ID || peer.Epoch != cmd.Endpoint.Epoch || peer.MediaVersion != cmd.MediaVersion {
		return false, ErrStale
	}
	if !m.Open() {
		return false, ErrStale
	}
	if !now.Before(m.Deadline) {
		return false, ErrConflict
	}
	switch cmd.Action {
	case "migration_ready":
		if m.Phase != "preparing" {
			return false, ErrConflict
		}
		if peer.Ready {
			return false, nil
		}
		peer.Ready = true
		if m.all(func(p MigrationPeer) bool { return p.Ready }) {
			m.Phase = "committing"
			m.Deadline = now.Add(MigrationCommitTimeout)
			c.Deadline = m.Deadline
		}
	case "migration_switched":
		if m.Phase != "committing" {
			return false, ErrConflict
		}
		if peer.Switched {
			return false, nil
		}
		peer.Switched = true
		if m.all(func(p MigrationPeer) bool { return p.Switched }) {
			c.Transport = "sfu"
			c.Generation = m.TargetGeneration
			c.NegotiationID = uuid.New()
			c.ConnectionDeadline = time.Time{}
			for i := range c.Participants {
				item := &c.Participants[i]
				item.MediaVersion = m.Peer(item.UserID).MediaVersion
				item.Ready = true
			}
			m.Phase = "cleaning"
			m.Deadline = now.Add(MigrationCleanupTimeout)
			c.Deadline = m.Deadline
		}
	case "migration_clean":
		if m.Phase != "cleaning" {
			return false, ErrConflict
		}
		if peer.Clean {
			return false, nil
		}
		peer.Clean = true
		if m.all(func(p MigrationPeer) bool { return p.Clean }) {
			m.Phase = "completed"
			m.FinishedAt = &now
			m.Deadline = time.Time{}
			c.Deadline = time.Time{}
		}
	case "migration_failed":
		if m.Phase == "cleaning" {
			c.finish(now, "migration_connection_failed")
		} else if m.Phase != "aborting" {
			beginAbort(c, "candidate_failed", now)
		} else {
			return false, nil
		}
	case "migration_abort_clean":
		if m.Phase != "aborting" {
			return false, ErrConflict
		}
		if peer.AbortClean {
			return false, nil
		}
		peer.AbortClean = true
		peer.SourceHealthy = cmd.Ready
		if m.all(func(p MigrationPeer) bool { return p.AbortClean }) {
			m.Phase = "aborted"
			m.FinishedAt = &now
			m.Deadline = time.Time{}
			c.Deadline = time.Time{}
			for i := range c.Participants {
				item := &c.Participants[i]
				item.Ready = m.Peer(item.UserID).SourceHealthy
			}
			if !m.all(func(p MigrationPeer) bool { return p.SourceHealthy }) {
				c.State = Connecting
				c.Deadline = now.Add(ReconnectTimeout)
			}
		}
	default:
		return false, ErrInvalid
	}
	c.Version++
	return true, nil
}
