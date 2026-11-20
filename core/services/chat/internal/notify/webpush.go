package notify

import (
	"context"
	"fmt"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const pushTTL = int(time.Hour / time.Second)

type WebPush struct {
	PublicKey  string
	PrivateKey string
	Subject    string
	Client     *http.Client
}

func (w WebPush) Send(ctx context.Context, sub domain.PushSubscription, payload []byte, topic string) (int, error) {
	return w.SendTTL(ctx, sub, payload, topic, pushTTL)
}

func (w WebPush) SendTTL(ctx context.Context, sub domain.PushSubscription, payload []byte, topic string, ttl int) (int, error) {
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256DH, Auth: sub.Auth},
	}, &webpush.Options{
		HTTPClient:      w.Client,
		Subscriber:      w.Subject,
		Topic:           topic,
		TTL:             ttl,
		Urgency:         webpush.UrgencyHigh,
		VAPIDPublicKey:  w.PublicKey,
		VAPIDPrivateKey: w.PrivateKey,
	})
	if err != nil {
		return 0, fmt.Errorf("notify: send push: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}
