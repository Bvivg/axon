package reqlimit_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"

	"github.com/bvivg/axon/core/services/gateway/internal/reqlimit"
)

type result struct {
	read     int
	err      error
	canceled bool
}

func serve(t *testing.T, readTimeout time.Duration, handler http.Handler) string {
	t.Helper()
	srv := httptest.NewUnstartedServer(handler)
	srv.Config.ReadHeaderTimeout = time.Second
	srv.Config.ReadTimeout = readTimeout
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func reader(sleep time.Duration, out chan<- result) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			out <- result{read: int(n), err: err}
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(w, "too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}
		select {
		case <-time.After(sleep):
		case <-r.Context().Done():
		}
		out <- result{read: int(n), canceled: r.Context().Err() != nil}
		w.WriteHeader(http.StatusNoContent)
	})
}

func trickle(t *testing.T, url string, size int, every time.Duration) {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", strings.TrimPrefix(url, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: gateway\r\nContent-Type: application/octet-stream\r\nContent-Length: %d\r\n\r\n", size); err != nil {
		t.Fatal(err)
	}
	go func() {
		for range size {
			if _, err := conn.Write([]byte("a")); err != nil {
				return
			}
			time.Sleep(every)
		}
	}()
}

func send(t *testing.T, method, url, body string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestAnOversizedBodyIsCutOff(t *testing.T) {
	seen := make(chan result, 1)
	url := serve(t, time.Second, reqlimit.Body(1024)(reader(0, seen)))

	status := send(t, http.MethodPost, url, strings.Repeat("a", 4096))

	got := <-seen
	var tooLarge *http.MaxBytesError
	if !errors.As(got.err, &tooLarge) || status != http.StatusRequestEntityTooLarge {
		t.Fatalf("got status %d and %v, want 413 from a MaxBytesError", status, got.err)
	}
	if got.read > 1024 {
		t.Fatalf("the handler read %d bytes past a 1024 byte limit", got.read)
	}
}

func TestASlowBodyTimesOut(t *testing.T) {
	seen := make(chan result, 1)
	url := serve(t, 200*time.Millisecond, reader(0, seen))
	trickle(t, url, 1000, 50*time.Millisecond)

	select {
	case got := <-seen:
		if got.err == nil {
			t.Fatal("a body that never finished was read without an error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a trickling body held the handler past the read timeout")
	}
}

func TestAnUploadRouteOutlivesTheReadTimeout(t *testing.T) {
	seen := make(chan result, 1)
	url := serve(t, 200*time.Millisecond, reqlimit.Deadline(5*time.Second)(reader(0, seen)))
	trickle(t, url, 20, 30*time.Millisecond)

	select {
	case got := <-seen:
		if got.err != nil || got.read != 20 {
			t.Fatalf("a slow upload inside its own deadline failed: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the upload never finished")
	}
}

func TestTheReadTimeoutStopsAtTheEndOfTheBody(t *testing.T) {
	seen := make(chan result, 1)
	url := serve(t, 100*time.Millisecond, reader(400*time.Millisecond, seen))

	status := send(t, http.MethodPost, url, `{"fast":true}`)

	got := <-seen
	if got.err != nil || got.canceled || status != http.StatusNoContent {
		t.Fatalf("a handler that outlived the read timeout after reading the body was cut short: %+v, status %d", got, status)
	}
}

func TestRequestsWithoutABodyAreLeftAlone(t *testing.T) {
	seen := make(chan result, 1)
	url := serve(t, 100*time.Millisecond, reader(400*time.Millisecond, seen))

	send(t, http.MethodGet, url, "")

	if got := <-seen; got.canceled {
		t.Fatal("a long GET (media download, websocket upgrade) was cancelled by the read timeout")
	}
}

type profiles struct {
	authv1connect.UnimplementedAuthServiceHandler
	calls int
}

func (p *profiles) GetUsersPublicProfiles(context.Context, *connect.Request[authv1.GetUsersPublicProfilesRequest]) (*connect.Response[authv1.GetUsersPublicProfilesResponse], error) {
	p.calls++
	return connect.NewResponse(&authv1.GetUsersPublicProfilesResponse{}), nil
}

func ids(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
	}
	return out
}

func TestALongRepeatedFieldIsRefused(t *testing.T) {
	stub := &profiles{}
	mux := http.NewServeMux()
	mux.Handle(authv1connect.NewAuthServiceHandler(stub, connect.WithInterceptors(reqlimit.Lists(100))))
	client := authv1connect.NewAuthServiceClient(http.DefaultClient, serve(t, time.Second, mux))

	if _, err := client.GetUsersPublicProfiles(t.Context(), connect.NewRequest(&authv1.GetUsersPublicProfilesRequest{UserIds: ids(100)})); err != nil {
		t.Fatalf("a list at the cap was refused: %v", err)
	}
	_, err := client.GetUsersPublicProfiles(t.Context(), connect.NewRequest(&authv1.GetUsersPublicProfilesRequest{UserIds: ids(101)}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a list over the cap: got %v, want invalid_argument", err)
	}
	if stub.calls != 1 {
		t.Fatalf("the upstream saw %d calls, want only the one inside the cap", stub.calls)
	}
}

func TestNestedListsAreCounted(t *testing.T) {
	positions := make([]int32, 101)
	nested := &chatv1.ForwardMessagesRequest{Parts: []*chatv1.ForwardPart{{MessageId: "m", Positions: positions}}}
	if err := reqlimit.CheckLists(nested, 100); err == nil {
		t.Fatal("a nested list over the cap passed")
	}

	flat := &chatv1.ForwardMessagesRequest{MessageIds: ids(100), RoomIds: ids(50)}
	if err := reqlimit.CheckLists(flat, 100); err != nil {
		t.Fatalf("lists inside the cap were refused: %v", err)
	}
	if err := reqlimit.CheckLists(&emptypb.Empty{}, 100); err != nil {
		t.Fatalf("an empty message was refused: %v", err)
	}
}
