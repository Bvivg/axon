package calls

import (
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalid     = errors.New("calls: invalid request")
	ErrForbidden   = errors.New("calls: forbidden")
	ErrNotFound    = errors.New("calls: not found")
	ErrBusy        = errors.New("calls: user is busy")
	ErrConflict    = errors.New("calls: state or request conflict")
	ErrStale       = errors.New("calls: stale endpoint or generation")
	ErrUnavailable = errors.New("calls: unavailable")
)

const (
	Ringing          = "ringing"
	Connecting       = "connecting"
	Active           = "active"
	Ended            = "ended"
	Lease            = 30 * time.Second
	RingTimeout      = 30 * time.Second
	SetupTimeout     = 10 * time.Second
	ReconnectTimeout = 15 * time.Second
	CleanupTimeout   = 3 * time.Second
)

type Actor struct {
	UserID, FamilyID uuid.UUID
	Token            string
}
type EndpointRef struct {
	ID    uuid.UUID
	Epoch int64
	Proof string
}

func (e EndpointRef) Hash() [32]byte { return sha256.Sum256([]byte(e.Proof)) }

type Endpoint struct {
	ID, UserID, FamilyID uuid.UUID
	Epoch                int64
	LeaseExpiresAt       time.Time
	SupportsSFU          bool
	SupportsMigration    bool
}
type Participant struct {
	SupportsMigration bool       `json:"supports_migration,omitempty"`
	SupportsSFU       bool       `json:"supports_sfu"`
	MediaVersion      int64      `json:"media_version"`
	InviteUntil       time.Time  `json:"invite_until,omitempty"`
	MediaDeadline     time.Time  `json:"media_deadline,omitempty"`
	CleanupAt         time.Time  `json:"cleanup_at,omitempty"`
	ReleaseAt         time.Time  `json:"release_at,omitempty"`
	LeftAt            *time.Time `json:"left_at,omitempty"`
	ConnectedAt       *time.Time `json:"connected_at,omitempty"`
	SourcePending     bool       `json:"source_pending,omitempty"`
	EndpointExpired   bool       `json:"-"`
	RetiredVersion    int64      `json:"retired_version,omitempty"`
	UserID            uuid.UUID  `json:"user_id"`
	Status            string     `json:"status"`
	EndpointID        uuid.UUID  `json:"endpoint_id"`
	Epoch             int64      `json:"epoch"`
	Ready             bool       `json:"ready"`
	CleanupDone       bool       `json:"cleanup_done"`
	CleanupExpired    bool       `json:"cleanup_expired"`
}
type Call struct {
	Migration          *Migration    `json:"migration,omitempty"`
	TransportSerial    int64         `json:"transport_serial,omitempty"`
	ConnectionDeadline time.Time     `json:"connection_deadline,omitempty"`
	Kind               string        `json:"kind,omitempty"`
	Transport          string        `json:"transport,omitempty"`
	TransportPhase     string        `json:"transport_phase,omitempty"`
	CleanupGeneration  int64         `json:"cleanup_generation,omitempty"`
	PredecessorID      uuid.UUID     `json:"predecessor_id,omitempty"`
	ReplacedByID       uuid.UUID     `json:"replaced_by_id,omitempty"`
	PredecessorViewers []uuid.UUID   `json:"predecessor_viewers,omitempty"`
	FallbackAvailable  bool          `json:"fallback_available,omitempty"`
	OwnerID            uuid.UUID     `json:"owner_id,omitempty"`
	ID                 uuid.UUID     `json:"id"`
	RoomID             uuid.UUID     `json:"room_id"`
	CallerID           uuid.UUID     `json:"caller_id"`
	StartKey           uuid.UUID     `json:"start_key"`
	Mode               string        `json:"mode"`
	State              string        `json:"state"`
	Version            int64         `json:"version"`
	Generation         int64         `json:"generation"`
	NegotiationID      uuid.UUID     `json:"negotiation_id"`
	EverActive         bool          `json:"ever_active"`
	CreatedAt          time.Time     `json:"created_at"`
	StartedAt          *time.Time    `json:"started_at,omitempty"`
	EndedAt            *time.Time    `json:"ended_at,omitempty"`
	Deadline           time.Time     `json:"deadline"`
	ReleaseAt          time.Time     `json:"release_at"`
	EndReason          string        `json:"end_reason"`
	Participants       []Participant `json:"participants"`
}
type Command struct {
	MigrationID           uuid.UUID
	EnableMigration       bool
	MigrationCapable      bool
	MediaVersion          int64
	Users                 []uuid.UUID
	TargetID              uuid.UUID
	EnableSFU             bool
	SFUCapable            bool
	Action                string
	ID, RoomID, RequestID uuid.UUID
	Endpoint              EndpointRef
	Mode                  string
	Generation            int64
	Ready                 bool
	Reason                string
}

func (cmd Command) StartsDirect() bool {
	return cmd.Action == "start" || cmd.Action == "start_direct"
}

type Event struct {
	ID, UserID uuid.UUID
	Sequence   int64
	Call       Call
}

func New(id, room, caller, peer, key uuid.UUID, mode string, now time.Time) Call {
	return Call{ID: id, RoomID: room, CallerID: caller, StartKey: key, Mode: mode, Generation: 1, NegotiationID: uuid.New(), CreatedAt: now, Participants: []Participant{{UserID: caller, Status: "invited"}, {UserID: peer, Status: "invited"}}}
}
func (c *Call) Participant(user uuid.UUID) *Participant {
	for i := range c.Participants {
		if c.Participants[i].UserID == user {
			return &c.Participants[i]
		}
	}
	return nil
}
func (c Call) Clean() bool {
	for _, p := range c.Participants {
		if p.SourcePending || (!p.CleanupDone && !p.CleanupExpired) {
			return false
		}
	}
	return true
}
func (c *Call) finish(now time.Time, reason string) {
	if c.Migration != nil && c.Migration.Open() {
		c.Migration.Phase = "aborted"
		c.Migration.Outcome = reason
		c.Migration.FinishedAt = &now
	}
	c.State = Ended
	c.EndReason = reason
	c.EndedAt = &now
	c.Deadline = now.Add(CleanupTimeout)
	c.ReleaseAt = now.Add(Lease)
	for i := range c.Participants {
		p := &c.Participants[i]
		p.Ready = false
		if p.EndpointID == uuid.Nil && !p.SourcePending {
			p.CleanupDone = true
		}
		if p.Status == "invited" {
			p.Status = reason
			p.RetiredVersion = c.Version + 1
		}
	}
}

func applyBase(c *Call, actor uuid.UUID, cmd Command, now time.Time) (bool, error) {
	if c.Kind == "group" || c.Transport == "sfu" {
		return ApplySFU(c, actor, cmd, now)
	}
	p := c.Participant(actor)
	if cmd.Action != "timeout" && p == nil {
		return false, ErrForbidden
	}
	if !cmd.StartsDirect() && cmd.Action != "cleanup" && cmd.Action != "timeout" && cmd.Action != "resume" && c.State == Ended {
		return false, nil
	}
	if cmd.Action != "timeout" && cmd.Action != "accept" && cmd.Action != "decline" && !cmd.StartsDirect() && (p.EndpointID != cmd.Endpoint.ID || p.Epoch != cmd.Endpoint.Epoch) && cmd.Action != "resume" {
		return false, ErrForbidden
	}
	switch cmd.Action {
	case "start", "start_direct":
		if c.State != "" || actor != c.CallerID || (c.Mode != "audio" && c.Mode != "video") {
			return false, ErrInvalid
		}
		p.Status = "accepted"
		p.SupportsSFU = cmd.SFUCapable
		p.SupportsMigration = cmd.MigrationCapable
		p.EndpointID = cmd.Endpoint.ID
		p.Epoch = cmd.Endpoint.Epoch
		c.State = Ringing
		c.Deadline = now.Add(RingTimeout)
	case "accept":
		if c.State != Ringing || actor == c.CallerID || !now.Before(c.Deadline) {
			return false, ErrConflict
		}
		p.Status = "accepted"
		p.SupportsSFU = cmd.SFUCapable
		p.SupportsMigration = cmd.MigrationCapable
		p.EndpointID = cmd.Endpoint.ID
		p.Epoch = cmd.Endpoint.Epoch
		c.State = Connecting
		c.Deadline = now.Add(SetupTimeout)
		c.FallbackAvailable = cmd.EnableSFU
		for _, other := range c.Participants {
			c.FallbackAvailable = c.FallbackAvailable && other.SupportsSFU
		}
	case "decline":
		if c.State != Ringing || actor == c.CallerID {
			return false, ErrConflict
		}
		p.Status = "declined"
		c.finish(now, "declined")
	case "cancel":
		if actor != c.CallerID || c.State != Ringing {
			return false, ErrConflict
		}
		c.finish(now, "cancelled")
	case "end":
		c.finish(now, "ended")
	case "ready":
		if c.State == Connecting && !now.Before(c.Deadline) {
			return false, ErrConflict
		}
		if cmd.Generation != c.Generation {
			return false, ErrStale
		}
		if c.State != Connecting && c.State != Active {
			return false, ErrConflict
		}
		if !cmd.Ready {
			if c.State == Connecting && !c.EverActive && c.FallbackAvailable && cmd.EnableSFU {
				if !p.Ready {
					return false, nil
				}
				p.Ready = false
				break
			}
			c.finish(now, "connection_failed")
			break
		}
		if p.Ready {
			return false, nil
		}
		p.Ready = true
		ready := true
		for _, other := range c.Participants {
			ready = ready && other.Ready
		}
		if ready {
			c.State = Active
			c.EverActive = true
			c.Deadline = time.Time{}
			if c.StartedAt == nil {
				c.StartedAt = &now
			}
		}
	case "cleanup":
		if c.State != Ended || cmd.Generation != c.Generation {
			return false, ErrConflict
		}
		if p.CleanupDone {
			return false, nil
		}
		p.CleanupDone = true
	case "resume":
		if p.EndpointID != cmd.Endpoint.ID || p.Epoch == cmd.Endpoint.Epoch {
			return false, nil
		}
		if p.Epoch > cmd.Endpoint.Epoch {
			return false, ErrStale
		}
		p.Epoch = cmd.Endpoint.Epoch
		p.SupportsSFU = cmd.SFUCapable
		p.SupportsMigration = cmd.MigrationCapable
		c.FallbackAvailable = cmd.EnableSFU
		for _, other := range c.Participants {
			c.FallbackAvailable = c.FallbackAvailable && other.SupportsSFU
		}
		if c.State == Ended {
			break
		}
		if c.TransportSerial > c.Generation {
			c.Generation = c.TransportSerial
		}
		c.Generation++
		c.NegotiationID = uuid.New()
		for i := range c.Participants {
			c.Participants[i].Ready = false
		}
		if c.State != Ringing {
			c.State = Connecting
			c.Deadline = now.Add(ReconnectTimeout)
		}
	case "timeout":
		if c.State == Connecting && !c.EverActive && c.FallbackAvailable && cmd.EnableSFU && cmd.Reason == "" {
			BeginFallback(c, now)
			c.Version++
			return true, nil
		}
		if c.State == Ended {
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
				}
				c.Deadline = time.Time{}
			}
		} else {
			reason := cmd.Reason
			if reason == "" {
				reason = "setup_timeout"
				if c.State == Ringing {
					reason = "missed"
				} else if c.EverActive {
					reason = "reconnect_timeout"
				}
			}
			c.finish(now, reason)
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
