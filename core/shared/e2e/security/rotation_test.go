//go:build e2e

package security_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
)

func keyID(t *testing.T, token string) string {
	t.Helper()
	header, _, _ := strings.Cut(token, ".")
	raw, err := base64.RawURLEncoding.DecodeString(header)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed.Kid
}

func TestTokensSurviveSigningKeyRotationUnderLoad(t *testing.T) {
	window, err := time.ParseDuration(os.Getenv("JWT_ROTATION_WATCH"))
	if err != nil || window <= 0 {
		t.Skip("JWT_ROTATION_WATCH is not set")
	}
	gateway := env(t, "GATEWAY_URL", "http://gateway:8080")
	client := &http.Client{Timeout: 10 * time.Second}
	auth := authv1connect.NewAuthServiceClient(client, gateway)
	ctx := t.Context()
	email := "rotation-" + uuid.NewString()[:8] + "@axon.test"
	password := "rotation-" + uuid.NewString()
	if _, err := auth.Register(ctx, connect.NewRequest(&authv1.RegisterRequest{Email: email, Password: password})); err != nil {
		t.Fatal(err)
	}
	var (
		mu     sync.Mutex
		tokens []string
		kids   = map[string]bool{}
		calls  atomic.Int64
		failed atomic.Int64
	)
	login := func() {
		res, err := auth.Login(ctx, connect.NewRequest(&authv1.LoginRequest{Email: email, Password: password}))
		if err != nil {
			t.Error("login:", err)
			return
		}
		token := res.Msg.GetTokens().GetAccessToken()
		mu.Lock()
		tokens = append(tokens, token)
		kids[keyID(t, token)] = true
		mu.Unlock()
	}
	login()
	stop := time.Now().Add(window)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for time.Now().Before(stop) {
				mu.Lock()
				live := append([]string(nil), tokens...)
				mu.Unlock()
				for _, token := range live {
					req := connect.NewRequest(&authv1.GetMeRequest{})
					req.Header().Set("Authorization", "Bearer "+token)
					calls.Add(1)
					if _, err := auth.GetMe(ctx, req); err != nil {
						failed.Add(1)
						t.Error("GetMe with a live token:", keyID(t, token), err)
					}
				}
				time.Sleep(100 * time.Millisecond)
			}
		})
	}
	for time.Now().Before(stop) {
		time.Sleep(15 * time.Second)
		login()
	}
	wg.Wait()
	t.Logf("%d calls, %d failed, signing keys seen: %d", calls.Load(), failed.Load(), len(kids))
	if len(kids) < 3 {
		t.Fatal("expected at least three signing keys during the window", kids)
	}
}
