package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/internalrpc"
	"github.com/bvivg/axon/core/shared/pkg/mtls"
)

const OwnerPeerIdentity = "game"

var errOwnerCaller = connect.NewError(connect.CodePermissionDenied, errors.New("only game instances may call the owner service"))

func (h *Handler) GuardOwner(next http.Handler) http.Handler {
	errors := connect.NewErrorWriter()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/"+gamev1connect.GameOwnerServiceName+"/") && !h.ownerCaller(r) {
			_ = errors.Write(w, r, errOwnerCaller)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) ownerCaller(r *http.Request) bool {
	if h.scheme == "https" {
		return r.TLS != nil && len(r.TLS.PeerCertificates) > 0 &&
			mtls.Authorize(r.TLS.PeerCertificates[0], []string{OwnerPeerIdentity}) == nil
	}
	return internalrpc.Authorized(r.Header, h.ownerKey)
}

func ownerKeyInterceptor(key string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if key != "" {
				req.Header().Set(internalrpc.Header, key)
			}
			return next(ctx, req)
		}
	}
}
