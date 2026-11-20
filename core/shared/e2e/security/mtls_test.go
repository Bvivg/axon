//go:build e2e

package security_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
)

func env(t *testing.T, key, fallback string) string {
	t.Helper()
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func targets(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, item := range strings.Split(env(t, "MTLS_TARGETS", "auth=auth:8081,chat=chat:8082,game=game:8083,rating=rating:8084"), ",") {
		name, address, _ := strings.Cut(item, "=")
		out[name] = address
	}
	return out
}

func roots(t *testing.T) *x509.CertPool {
	t.Helper()
	pem, err := os.ReadFile(filepath.Join(env(t, "MTLS_INTRUDER_DIR", "/certs/intruder"), "ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatal("no CA certificate")
	}
	return pool
}

func probe(t *testing.T, client *http.Client, url string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("%s answered %d without a client certificate", url, res.StatusCode)
	}
	return nil
}

func procedure(name string) string {
	switch name {
	case "auth":
		return "/axon.auth.v1.AuthService/GetMe"
	case "chat":
		return "/axon.chat.v1.ChatService/ListRooms"
	case "rating":
		return "/axon.rating.v1.RatingService/GetLeaderboard"
	default:
		return "/axon.game.v1.GameService/ListSessions"
	}
}

func TestPlainHTTPToAnInternalServiceIsRefused(t *testing.T) {
	for name, address := range targets(t) {
		t.Run(name, func(t *testing.T) {
			if err := probe(t, &http.Client{}, "http://"+address+procedure(name)); err != nil {
				t.Logf("refused at transport level: %v", err)
			}
		})
	}
}

func TestTLSWithoutAClientCertificateIsRefused(t *testing.T) {
	pool := roots(t)
	for name, address := range targets(t) {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, ServerName: name}}}
			if err := probe(t, client, "https://"+address+procedure(name)); err == nil {
				t.Fatal("handshake without a client certificate succeeded")
			} else if !strings.Contains(err.Error(), "certificate required") {
				t.Fatal("expected the server to demand a certificate", err)
			}
		})
	}
}

func TestACertificateOutsideTheAllowListIsRefused(t *testing.T) {
	dir := env(t, "MTLS_INTRUDER_DIR", "/certs/intruder")
	pair, err := tls.LoadX509KeyPair(filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"))
	if err != nil {
		t.Fatal(err)
	}
	pool := roots(t)
	for name, address := range targets(t) {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, ServerName: name, Certificates: []tls.Certificate{pair}}}}
			if err := probe(t, client, "https://"+address+procedure(name)); err == nil {
				t.Fatal("a certificate from the same CA but outside the allow-list got through")
			} else if !strings.Contains(err.Error(), "bad certificate") {
				t.Fatal("expected the server to reject the identity", err)
			}
		})
	}
}

func TestTheGatewayStillReachesEveryService(t *testing.T) {
	gateway := env(t, "GATEWAY_URL", "http://gateway:8080")
	client := &http.Client{Timeout: 10 * time.Second}
	auth := authv1connect.NewAuthServiceClient(client, gateway)
	ctx := t.Context()
	email := "mtls-" + uuid.NewString()[:8] + "@axon.test"
	registered, err := auth.Register(ctx, connect.NewRequest(&authv1.RegisterRequest{Email: email, Password: "mtls-" + uuid.NewString()}))
	if err != nil {
		t.Fatal("register through the gateway:", err)
	}
	token := "Bearer " + registered.Msg.GetTokens().GetAccessToken()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		me := connect.NewRequest(&authv1.GetMeRequest{})
		me.Header().Set("Authorization", token)
		got, err := auth.GetMe(ctx, me)
		if err != nil || got.Msg.GetUser().GetEmail() != email {
			t.Fatal("auth through the gateway:", err)
		}
		rooms := connect.NewRequest(&chatv1.ListRoomsRequest{})
		rooms.Header().Set("Authorization", token)
		if _, err := chatv1connect.NewChatServiceClient(client, gateway).ListRooms(ctx, rooms); err != nil {
			t.Fatal("chat through the gateway:", err)
		}
		sessions := connect.NewRequest(&gamev1.ListSessionsRequest{})
		sessions.Header().Set("Authorization", token)
		if _, err := gamev1connect.NewGameServiceClient(client, gateway).ListSessions(ctx, sessions); err != nil {
			t.Fatal("game through the gateway:", err)
		}
		if os.Getenv("MTLS_WATCH_RENEWAL") == "" || time.Now().After(deadline) {
			return
		}
		time.Sleep(5 * time.Second)
	}
}

func TestCertificatesAreRenewedWithoutRestarts(t *testing.T) {
	if os.Getenv("MTLS_WATCH_RENEWAL") == "" {
		t.Skip("MTLS_WATCH_RENEWAL is not set")
	}
	address := targets(t)["auth"]
	serial := func() string {
		conn, err := tls.Dial("tcp", address, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots(t), ServerName: "auth"})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		return conn.ConnectionState().PeerCertificates[0].SerialNumber.String()
	}
	first := serial()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Second)
		if next := serial(); next != first {
			t.Logf("auth presented a renewed certificate: %s → %s", first, next)
			return
		}
	}
	t.Fatal("auth kept presenting the same certificate past its renewal window")
}
