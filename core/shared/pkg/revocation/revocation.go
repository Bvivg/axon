package revocation

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

func familyChannel(familyID uuid.UUID) string {
	return "auth:session-revoked:family:" + familyID.String()
}

func userChannel(userID uuid.UUID) string { return "auth:session-revoked:user:" + userID.String() }

type Publisher struct {
	client goredis.UniversalClient
}

func NewPublisher(client goredis.UniversalClient) (*Publisher, error) {
	if client == nil {
		return nil, errors.New("revocation: a redis client is required")
	}
	return &Publisher{client: client}, nil
}

func (p *Publisher) PublishFamily(ctx context.Context, familyID uuid.UUID) error {
	if err := p.client.Publish(ctx, familyChannel(familyID), "revoked").Err(); err != nil {
		return fmt.Errorf("revocation: publish family %s: %w", familyID, err)
	}
	return nil
}

func (p *Publisher) PublishUser(ctx context.Context, userID uuid.UUID) error {
	if err := p.client.Publish(ctx, userChannel(userID), "revoked").Err(); err != nil {
		return fmt.Errorf("revocation: publish user %s: %w", userID, err)
	}
	return nil
}

type Subscriber struct {
	client goredis.UniversalClient
}

func NewSubscriber(client goredis.UniversalClient) (*Subscriber, error) {
	if client == nil {
		return nil, errors.New("revocation: a redis client is required")
	}
	return &Subscriber{client: client}, nil
}

func (s *Subscriber) Subscribe(ctx context.Context, userID, familyID uuid.UUID) (<-chan struct{}, func(), error) {
	sub := s.client.Subscribe(ctx, userChannel(userID), familyChannel(familyID))

	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, nil, fmt.Errorf("revocation: subscribe: %w", err)
	}

	signal := make(chan struct{}, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for range sub.Channel() {
			select {
			case signal <- struct{}{}:
			default:
			}
		}
	}()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = sub.Close()
			<-done
		})
	}

	return signal, stop, nil
}
