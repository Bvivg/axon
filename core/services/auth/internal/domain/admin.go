package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"
)

type UserStatusFilter int

const (
	UserStatusAny UserStatusFilter = iota
	UserStatusActive
	UserStatusSuspended
)

type UserSort int

const (
	UserSortNewest UserSort = iota
	UserSortOldest
	UserSortEmail
)

const (
	DefaultAdminPage = 25
	MaxAdminPage     = 100
)

type UserFilter struct {
	Query  string
	Role   string
	Status UserStatusFilter
	Sort   UserSort
	Cursor string
	Limit  int
	List   adminquery.Request
}

type AdminUser struct {
	User           User
	ActiveSessions int
	Version        int64
}

type UserPage struct {
	Users []AdminUser
	Next  string
	Total int64
}

type AuditOutcome string

const (
	AuditSucceeded AuditOutcome = "succeeded"
	AuditDenied    AuditOutcome = "denied"
	AuditFailed    AuditOutcome = "failed"
)

type AuditEvent struct {
	ID            uuid.UUID
	OccurredAt    time.Time
	Service       string
	Action        string
	Outcome       AuditOutcome
	ActorID       *uuid.UUID
	TargetType    string
	TargetID      string
	Reason        string
	CorrelationID string
	ClientIP      string
	UserAgent     string
	Details       map[string]string
	Changes       []AuditChange
}

type AuditChange struct {
	Field  string `json:"field"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

type AuditFilter struct {
	ActorID    *uuid.UUID
	TargetType string
	TargetID   string
	Service    string
	Action     string
	Outcome    AuditOutcome
	Since      *time.Time
	Until      *time.Time
	Cursor     string
	Limit      int
	List       adminquery.Request
}

type AuditPage struct {
	Events []AuditEvent
	Next   string
	Total  *int64
}

func PageSize(requested int) int {
	switch {
	case requested <= 0:
		return DefaultAdminPage
	case requested > MaxAdminPage:
		return MaxAdminPage
	default:
		return requested
	}
}

const MaxAuditExport = 10000

type AdminRole struct {
	ID          uuid.UUID
	Name        string
	Title       string
	Permissions []string
	System      bool
	Members     int64
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type RoleFields struct {
	Title       bool
	Permissions bool
}

type LinkedAccount struct {
	Provider Provider
	Email    string
	LinkedAt time.Time
}
