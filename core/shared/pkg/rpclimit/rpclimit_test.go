package rpclimit_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/bvivg/axon/core/shared/pkg/rpclimit"
)

const procedure = "/axon.test.v1.EchoService/Echo"

const limit = 64 << 10

type server struct {
	url   string
	calls *atomic.Int64
}

func newServer(t *testing.T, options ...connect.HandlerOption) server {
	t.Helper()
	calls := new(atomic.Int64)
	handler := connect.NewUnaryHandler(procedure, func(_ context.Context, req *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
		calls.Add(1)
		return connect.NewResponse(wrapperspb.String(req.Msg.GetValue())), nil
	}, options...)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return server{url: srv.URL + procedure, calls: calls}
}

func member(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func bomb(t *testing.T, megabytes int) []byte {
	t.Helper()
	filler := member(t, bytes.Repeat([]byte("a"), 1<<20))
	out := member(t, []byte(`{"value":"`))
	for range megabytes {
		out = append(out, filler...)
	}
	return append(out, member(t, []byte(`"}`))...)
}

func post(t *testing.T, url string, body []byte, gzipped bool) (int, string, time.Duration) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if gzipped {
		req.Header.Set("Content-Encoding", "gzip")
	}
	started := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(out), time.Since(started)
}

func TestAGzipBombIsRejectedWithoutBeingInflated(t *testing.T) {
	payload := bomb(t, 200)
	if len(payload) > rpclimit.DefaultReadMaxBytes {
		t.Fatalf("the bomb is %d bytes compressed, it must fit under the %d byte limit to reach the decompressor", len(payload), rpclimit.DefaultReadMaxBytes)
	}

	srv := newServer(t, rpclimit.Handler(rpclimit.DefaultReadMaxBytes))
	status, body, took := post(t, srv.url, payload, true)

	if status != http.StatusTooManyRequests || !strings.Contains(body, `"code":"resource_exhausted"`) {
		t.Fatalf("got %d %s, want 429 resource_exhausted", status, body)
	}
	if !strings.Contains(body, rpclimit.ErrInflatedTooLarge.Error()) {
		t.Fatalf("the rejection %s did not come from the bounded decompressor", body)
	}
	if srv.calls.Load() != 0 {
		t.Fatal("the handler ran on a request that should have been refused")
	}

	inflate := time.Now()
	zr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, zr); err != nil {
		t.Fatal(err)
	}
	full := time.Since(inflate)
	if took > full/4 {
		t.Fatalf("the rejection took %v while inflating the whole bomb takes %v: the server inflated it", took, full)
	}
}

func TestTheStockHandlerInflatesTheWholeBomb(t *testing.T) {
	srv := newServer(t, connect.WithReadMaxBytes(limit))
	status, body, _ := post(t, srv.url, bomb(t, 32), true)

	if status != http.StatusTooManyRequests {
		t.Fatalf("got %d %s, want 429", status, body)
	}
	if !strings.Contains(body, fmt.Sprintf("message size %d is larger", 32<<20+len(`{"value":""}`))) {
		t.Fatalf("connect's own limit should drain and count the whole bomb, got %s", body)
	}
}

func TestAnOversizedPlainBodyIsRejected(t *testing.T) {
	srv := newServer(t, rpclimit.Handler(limit))
	status, body, _ := post(t, srv.url, []byte(`{"value":"`+strings.Repeat("a", limit)+`"}`), false)

	if status != http.StatusTooManyRequests || !strings.Contains(body, `"code":"resource_exhausted"`) {
		t.Fatalf("got %d %s, want 429 resource_exhausted", status, body)
	}
	if srv.calls.Load() != 0 {
		t.Fatal("the handler ran on an oversized request")
	}
}

func TestGzipStillWorksBothWaysUnderTheLimit(t *testing.T) {
	srv := newServer(t, rpclimit.Handler(limit))
	value := strings.Repeat("hello ", 4000)
	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](http.DefaultClient, srv.url, connect.WithSendGzip(), connect.WithCompressMinBytes(1))

	resp, err := client.CallUnary(t.Context(), connect.NewRequest(wrapperspb.String(value)))
	if err != nil {
		t.Fatalf("a compressed request under the limit was refused: %v", err)
	}
	if resp.Msg.GetValue() != value {
		t.Fatal("the echoed value does not match")
	}
}

func TestAMessageExactlyAtTheLimitIsAccepted(t *testing.T) {
	srv := newServer(t, rpclimit.Handler(limit))
	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](http.DefaultClient, srv.url, connect.WithSendGzip(), connect.WithCompressMinBytes(1), connect.WithProtoJSON())
	sized := func(n int) *wrapperspb.StringValue {
		wire, err := protojson.Marshal(wrapperspb.String(""))
		if err != nil {
			t.Fatal(err)
		}
		return wrapperspb.String(strings.Repeat("a", n-len(wire)))
	}

	if _, err := client.CallUnary(t.Context(), connect.NewRequest(sized(limit))); err != nil {
		t.Fatalf("a message of exactly the limit was refused: %v", err)
	}
	_, err := client.CallUnary(t.Context(), connect.NewRequest(sized(limit+1)))
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("one byte over the limit: got %v, want resource_exhausted", err)
	}
}

func TestTheDecompressorStopsAtTheLimit(t *testing.T) {
	decompressor := rpclimit.NewDecompressor(1024)
	if err := decompressor.Reset(bytes.NewReader(bomb(t, 32))); err != nil {
		t.Fatal(err)
	}

	n, err := io.Copy(io.Discard, decompressor)
	if !errors.Is(err, rpclimit.ErrInflatedTooLarge) {
		t.Fatalf("got %v, want ErrInflatedTooLarge", err)
	}
	if n != 1024 {
		t.Fatalf("inflated %d bytes, want exactly the 1024 byte budget", n)
	}

	if err := decompressor.Reset(bytes.NewReader(member(t, []byte(`{"value":"aaaaaaaaaa"}`)))); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(decompressor)
	if err != nil || string(out) != `{"value":"aaaaaaaaaa"}` {
		t.Fatalf("a reset decompressor read %q, %v", out, err)
	}
}

func TestAZeroLimitFallsBackToTheDefault(t *testing.T) {
	srv := newServer(t, rpclimit.Handler(0))
	status, _, _ := post(t, srv.url, []byte(`{"value":"`+strings.Repeat("a", rpclimit.DefaultReadMaxBytes)+`"}`), false)

	if status != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429 at the default limit", status)
	}
}
