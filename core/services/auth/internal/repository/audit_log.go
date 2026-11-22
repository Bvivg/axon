package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func (r *Repository) InsertAuditEvent(ctx context.Context, e domain.AuditEvent) (bool, error) {
	details := e.Details
	if details == nil {
		details = map[string]string{}
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return false, fmt.Errorf("repository: encode audit details: %w", err)
	}
	changes := e.Changes
	if changes == nil {
		changes = []domain.AuditChange{}
	}
	rawChanges, err := json.Marshal(changes)
	if err != nil {
		return false, fmt.Errorf("repository: encode audit changes: %w", err)
	}
	const query = `
		INSERT INTO audit_log (id, occurred_at, service, action, outcome, actor_id, target_type, target_id,
			reason, correlation_id, client_ip, user_agent, details, changes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (id) DO NOTHING`
	tag, err := r.q.Exec(ctx, query, e.ID, e.OccurredAt, e.Service, e.Action, string(e.Outcome), e.ActorID,
		e.TargetType, e.TargetID, e.Reason, e.CorrelationID, e.ClientIP, e.UserAgent, raw, rawChanges)
	if err != nil {
		return false, fmt.Errorf("repository: insert audit event: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

type auditCursor struct {
	At time.Time `json:"t"`
	ID uuid.UUID `json:"i"`
}

func auditWhere(f domain.AuditFilter) where {
	var filter where
	if f.ActorID != nil {
		filter.add(`actor_id = ?`, *f.ActorID)
	}
	if f.TargetType != "" {
		filter.add(`target_type = ?`, f.TargetType)
	}
	if f.TargetID != "" {
		filter.add(`target_id = ?`, f.TargetID)
	}
	if f.Service != "" {
		filter.add(`service = ?`, f.Service)
	}
	if f.Action != "" {
		filter.add(`action = ?`, f.Action)
	}
	if f.Outcome != "" {
		filter.add(`outcome = ?`, string(f.Outcome))
	}
	if f.Since != nil {
		filter.add(`occurred_at >= ?`, *f.Since)
	}
	if f.Until != nil {
		filter.add(`occurred_at < ?`, *f.Until)
	}
	return filter
}

const auditColumns = `id, occurred_at, service, action, outcome, actor_id, target_type, target_id, reason,
			correlation_id, client_ip, user_agent, details, changes`

func (r *Repository) ExportAuditEvents(ctx context.Context, f domain.AuditFilter, limit int) ([]domain.AuditEvent, error) {
	filter := auditWhere(f)
	listed, err := auditSchema.Compile(f.List.Filter, f.List.OrderBy)
	if err != nil {
		return nil, err
	}
	filter.addQuery(listed)
	filter.args = append(filter.args, limit)
	query := `SELECT ` + auditColumns + ` FROM audit_log` + filter.sql() + `
		ORDER BY ` + listed.Order + `
		LIMIT $` + strconv.Itoa(len(filter.args))
	return r.auditEvents(ctx, query, filter.args)
}

func (r *Repository) auditEvents(ctx context.Context, query string, args []any) ([]domain.AuditEvent, error) {
	rows, err := r.q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list audit events: %w", err)
	}
	defer rows.Close()
	events := []domain.AuditEvent{}
	for rows.Next() {
		var (
			e       domain.AuditEvent
			outcome string
			details []byte
			changes []byte
		)
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.Service, &e.Action, &outcome, &e.ActorID, &e.TargetType,
			&e.TargetID, &e.Reason, &e.CorrelationID, &e.ClientIP, &e.UserAgent, &details, &changes); err != nil {
			return nil, fmt.Errorf("repository: scan audit event: %w", err)
		}
		e.Outcome = domain.AuditOutcome(outcome)
		if len(details) > 0 {
			if err := json.Unmarshal(details, &e.Details); err != nil {
				return nil, fmt.Errorf("repository: decode audit details: %w", err)
			}
		}
		if len(changes) > 0 {
			if err := json.Unmarshal(changes, &e.Changes); err != nil {
				return nil, fmt.Errorf("repository: decode audit changes: %w", err)
			}
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list audit events: %w", err)
	}
	return events, nil
}

var auditSchema = adminquery.NewSchema(`occurred_at DESC, id DESC`, `id DESC`,
	adminquery.Field{Name: "when", Column: "occurred_at", Kind: adminquery.Time, Sortable: true},
	adminquery.Field{Name: "service", Column: "service", Kind: adminquery.String, Sortable: true},
	adminquery.Field{Name: "action", Column: "action", Kind: adminquery.String, Sortable: true},
	adminquery.Field{Name: "outcome", Column: "outcome", Kind: adminquery.Enum, Sortable: true, Values: map[string]string{
		"succeeded": "outcome = 'succeeded'",
		"denied":    "outcome = 'denied'",
		"failed":    "outcome = 'failed'",
	}},
	adminquery.Field{Name: "actor", Column: "actor_id", Kind: adminquery.UUID},
	adminquery.Field{Name: "target_type", Column: "target_type", Kind: adminquery.String, Sortable: true},
	adminquery.Field{Name: "target", Column: "target_id", Kind: adminquery.String},
	adminquery.Field{Name: "reason", Column: "reason", Kind: adminquery.String},
	adminquery.Field{Name: "client_ip", Column: "client_ip", Kind: adminquery.String},
)

func (r *Repository) ListAuditEvents(ctx context.Context, f domain.AuditFilter) (domain.AuditPage, error) {
	filter := auditWhere(f)
	listed, err := filter.prepare(auditSchema, f.List)
	if err != nil {
		return domain.AuditPage{}, err
	}
	if f.List.Paged() {
		var total int64
		if err := r.q.QueryRow(ctx, `SELECT count(*) FROM audit_log`+filter.sql(), filter.args...).Scan(&total); err != nil {
			return domain.AuditPage{}, fmt.Errorf("repository: count audit events: %w", err)
		}
		limit, offset := f.List.Page()
		filter.args = append(filter.args, limit, offset)
		query := `SELECT ` + auditColumns + ` FROM audit_log` + filter.sql() + `
			ORDER BY ` + listed.Order + `
			LIMIT $` + strconv.Itoa(len(filter.args)-1) + ` OFFSET $` + strconv.Itoa(len(filter.args))
		events, err := r.auditEvents(ctx, query, filter.args)
		if err != nil {
			return domain.AuditPage{}, err
		}
		return domain.AuditPage{Events: events, Total: &total}, nil
	}
	if f.Cursor != "" {
		var c auditCursor
		if err := decodeCursor(f.Cursor, &c); err != nil {
			return domain.AuditPage{}, err
		}
		filter.add(`(occurred_at, id) < (?, ?)`, c.At, c.ID)
	}

	limit := domain.PageSize(f.Limit)
	filter.args = append(filter.args, limit+1)
	query := `SELECT ` + auditColumns + ` FROM audit_log` + filter.sql() + `
		ORDER BY occurred_at DESC, id DESC
		LIMIT $` + strconv.Itoa(len(filter.args))
	events, err := r.auditEvents(ctx, query, filter.args)
	if err != nil {
		return domain.AuditPage{}, err
	}
	page := domain.AuditPage{Events: events}
	if len(page.Events) > limit {
		page.Events = page.Events[:limit]
		last := page.Events[limit-1]
		page.Next = encodeCursor(auditCursor{At: last.OccurredAt, ID: last.ID})
	}
	return page, nil
}
