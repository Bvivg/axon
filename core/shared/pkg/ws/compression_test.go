package ws_test

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/ws"
)

type countedConn struct {
	net.Conn
	written *atomic.Int64
}

func (c *countedConn) Write(data []byte) (int, error) {
	n, err := c.Conn.Write(data)
	c.written.Add(int64(n))
	return n, err
}

type compressionTransport struct {
	http.RoundTripper
	extensions string
}

func (t *compressionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.RoundTripper.RoundTrip(r)
	if err == nil {
		t.extensions = response.Header.Get("Sec-WebSocket-Extensions")
	}
	return response, err
}

func TestCompressionRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name   string
		client bool
		server bool
	}{
		{name: "disabled"},
		{name: "enabled", client: true, server: true},
		{name: "server does not support compression", client: true},
		{name: "client does not support compression", server: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enabled := tc.client && tc.server
			srv := echoServer(t, ws.AcceptOptions{Options: ws.Options{Compression: tc.server, ReadLimit: ws.LargeMessageReadLimit}})
			var written atomic.Int64
			transport := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				return &countedConn{Conn: conn, written: &written}, nil
			}}
			t.Cleanup(transport.CloseIdleConnections)
			recorder := &compressionTransport{RoundTripper: transport}
			conn, err := ws.Dial(t.Context(), srv.URL, ws.DialOptions{
				Options: ws.Options{Compression: tc.client, ReadLimit: ws.LargeMessageReadLimit}, HTTPClient: &http.Client{Transport: recorder},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.CloseNow() }()
			if negotiated := strings.Contains(recorder.extensions, "permessage-deflate"); negotiated != enabled {
				t.Fatalf("compression negotiated = %v, want %v", negotiated, enabled)
			}
			body := strings.Repeat("abcdefgh", 8192)
			before := written.Load()
			if err := conn.Write(t.Context(), []byte(body)); err != nil {
				t.Fatal(err)
			}
			wireBytes := written.Load() - before
			got, err := conn.Read(t.Context())
			if err != nil || string(got) != body {
				t.Fatalf("round trip: length=%d, error=%v", len(got), err)
			}
			if enabled && wireBytes >= int64(len(body))/4 {
				t.Fatalf("compression ineffective: %d bytes for %d bytes of text", wireBytes, len(body))
			}
			if !enabled && wireBytes < int64(len(body)) {
				t.Fatalf("uncompressed message unexpectedly shrank: %d", wireBytes)
			}
			t.Logf("text=%d bytes, wire=%d bytes, compression=%v", len(body), wireBytes, enabled)
		})
	}
}
