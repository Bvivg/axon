//go:build e2e

package security_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

func TestTheGameSocketReachesTheOwnerThroughTheGateway(t *testing.T) {
	gateway := env(t, "GATEWAY_URL", "http://gateway:8080")
	client := &http.Client{Timeout: 10 * time.Second}
	ctx := t.Context()
	registered, err := authv1connect.NewAuthServiceClient(client, gateway).Register(ctx, connect.NewRequest(&authv1.RegisterRequest{
		Email: "socket-" + uuid.NewString()[:8] + "@axon.test", Password: "socket-" + uuid.NewString(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	token := registered.Msg.GetTokens().GetAccessToken()
	create := connect.NewRequest(&gamev1.CreateSessionRequest{Game: "tictactoe", RequestId: uuid.NewString()})
	create.Header().Set("Authorization", "Bearer "+token)
	created, err := gamev1connect.NewGameServiceClient(client, gateway).CreateSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	id := created.Msg.GetSession().GetId()
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := ws.Dial(dialCtx, strings.Replace(gateway, "http", "ws", 1)+"/ws/game", ws.DialOptions{
		Subprotocols: []string{"axon.game.v1", "axon.bearer." + token},
	})
	if err != nil {
		t.Fatal("game socket through the gateway:", err)
	}
	defer func() { _ = conn.CloseNow() }()
	frame, _ := protojson.Marshal(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_Subscribe{Subscribe: id}})
	if err := conn.Write(dialCtx, frame); err != nil {
		t.Fatal(err)
	}
	raw, err := conn.Read(dialCtx)
	if err != nil {
		t.Fatal("no session state over the relayed socket:", err)
	}
	var event gamev1.SessionEvent
	if err := protojson.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	if event.GetSession().GetId() != id {
		t.Fatalf("expected the session snapshot, got %s", raw)
	}
}
