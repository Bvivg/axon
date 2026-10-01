package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func (r *Repository) SavePushSubscription(ctx context.Context, s domain.PushSubscription) error {
	const query = `
		INSERT INTO push_subscriptions (endpoint, user_id, p256dh, auth)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (endpoint) DO UPDATE
		SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth, created_at = now()`

	if _, err := r.q.Exec(ctx, query, s.Endpoint, s.UserID, s.P256DH, s.Auth); err != nil {
		return fmt.Errorf("repository: save push subscription: %w", err)
	}
	return nil
}

func (r *Repository) DeletePushSubscription(ctx context.Context, userID uuid.UUID, endpoint string) error {
	if _, err := r.q.Exec(ctx, `DELETE FROM push_subscriptions WHERE endpoint = $1 AND user_id = $2`, endpoint, userID); err != nil {
		return fmt.Errorf("repository: delete push subscription: %w", err)
	}
	return nil
}

func (r *Repository) DropPushEndpoint(ctx context.Context, endpoint string) error {
	if _, err := r.q.Exec(ctx, `DELETE FROM push_subscriptions WHERE endpoint = $1`, endpoint); err != nil {
		return fmt.Errorf("repository: drop push endpoint: %w", err)
	}
	return nil
}

func (r *Repository) PushSubscriptions(ctx context.Context, userIDs []uuid.UUID) ([]domain.PushSubscription, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := r.q.Query(ctx, `SELECT endpoint, user_id, p256dh, auth FROM push_subscriptions WHERE user_id = ANY($1)`, userIDs)
	if err != nil {
		return nil, fmt.Errorf("repository: push subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []domain.PushSubscription
	for rows.Next() {
		var s domain.PushSubscription
		if err := rows.Scan(&s.Endpoint, &s.UserID, &s.P256DH, &s.Auth); err != nil {
			return nil, fmt.Errorf("repository: scan push subscription: %w", err)
		}
		subs = append(subs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: push subscriptions: %w", err)
	}
	return subs, nil
}
