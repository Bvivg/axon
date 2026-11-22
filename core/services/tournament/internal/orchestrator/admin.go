package orchestrator

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	"github.com/bvivg/axon/core/services/tournament/internal/store"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

type StaleError struct {
	Current int64
}

func (e *StaleError) Error() string {
	return fmt.Sprintf("orchestrator: the tournament changed, current version %d", e.Current)
}

type Audit func(before, after saga.Tournament) outbox.Event

func (o *Orchestrator) Revise(ctx context.Context, id uuid.UUID, version int64, revise func(saga.Spec) saga.Spec, audit Audit) (saga.Tournament, saga.Tournament, error) {
	var before saga.Tournament
	after, err := o.store.Update(ctx, id, func(ctx context.Context, tx *store.Tx, t saga.Tournament) (saga.Tournament, error) {
		before = t
		next, changed, err := t.Revise(revise(t.Spec))
		if err != nil || !changed {
			return t, err
		}
		if t.Version != version {
			return t, &StaleError{Current: t.Version}
		}
		return next, tx.Publish(ctx, audit(t, next))
	})
	if err != nil {
		return saga.Tournament{}, saga.Tournament{}, err
	}
	return before, after, nil
}

func (o *Orchestrator) Remove(ctx context.Context, id, user uuid.UUID, audit Audit) (saga.Tournament, error) {
	return o.store.Update(ctx, id, func(ctx context.Context, tx *store.Tx, t saga.Tournament) (saga.Tournament, error) {
		next, changed, err := t.Withdraw(user)
		if err != nil {
			return t, err
		}
		if !changed {
			return t, saga.ErrNotRegistered
		}
		return next, tx.Publish(ctx, audit(t, next))
	})
}

func (o *Orchestrator) AdminList(ctx context.Context, f store.AdminFilter) (store.AdminPage, error) {
	return o.store.AdminList(ctx, f)
}
