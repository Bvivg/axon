//go:build e2e

package e2e

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
)

func browserKeys(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	secret := make([]byte, 16)
	_, _ = rand.Read(secret)
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(secret)
}

func TestADeviceSubscribesToPushWithTheServersKeyAndLeavesAgain(t *testing.T) {
	ctx := context.Background()
	alice, bob := newUser(t), newUser(t)

	key, err := alice.chat.GetPushKey(ctx, connect.NewRequest(&chatv1.GetPushKeyRequest{}))
	if err != nil {
		t.Fatalf("get push key: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(key.Msg.GetPublicKey())
	if err != nil || len(raw) != 65 || raw[0] != 0x04 {
		t.Fatalf("public key = %q, want an uncompressed P-256 point in base64url", key.Msg.GetPublicKey())
	}

	p256dh, secret := browserKeys(t)
	endpoint := "https://push.example.test/send/" + uuid.NewString()

	_, err = alice.chat.SubscribePush(ctx, connect.NewRequest(&chatv1.SubscribePushRequest{
		Endpoint: "http://push.example.test/plain", P256Dh: p256dh, Auth: secret,
	}))
	if code := connect.CodeOf(err); code != connect.CodeInvalidArgument {
		t.Fatalf("plain http endpoint: code = %v, want invalid_argument", code)
	}

	_, err = alice.chat.SubscribePush(ctx, connect.NewRequest(&chatv1.SubscribePushRequest{Endpoint: endpoint, P256Dh: p256dh}))
	if code := connect.CodeOf(err); code != connect.CodeInvalidArgument {
		t.Fatalf("missing auth secret: code = %v, want invalid_argument", code)
	}

	for _, who := range []*user{alice, alice, bob} {
		if _, err := who.chat.SubscribePush(ctx, connect.NewRequest(&chatv1.SubscribePushRequest{
			Endpoint: endpoint, P256Dh: p256dh, Auth: secret,
		})); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
	}

	for _, who := range []*user{alice, bob, bob} {
		if _, err := who.chat.UnsubscribePush(ctx, connect.NewRequest(&chatv1.UnsubscribePushRequest{Endpoint: endpoint})); err != nil {
			t.Fatalf("unsubscribe: %v", err)
		}
	}

	stranger := chatv1connect.NewChatServiceClient(&http.Client{Transport: forwardedFor{addr: "10.250.0.1"}}, gatewayURL)
	_, err = stranger.SubscribePush(ctx, connect.NewRequest(&chatv1.SubscribePushRequest{Endpoint: endpoint, P256Dh: p256dh, Auth: secret}))
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeUnauthenticated {
		t.Fatalf("anonymous subscribe: err = %v, want unauthenticated", err)
	}
}
