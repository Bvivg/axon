package authz

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

func OutboxRecorder(db outbox.Execer, table string) Recorder {
	return func(ctx context.Context, action Action) error {
		return outbox.Write(ctx, db, table, action.Event(uuid.New()))
	}
}

type Verifier interface {
	Verify(raw string) (authn.Claims, error)
}

type Recorder func(ctx context.Context, action Action) error

type Rules map[string]Permission

type Actor struct {
	UserID      uuid.UUID
	Permissions Set
	ClientIP    string
	UserAgent   string
}

type actorKey struct{}

func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func ActorFrom(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorKey{}).(Actor)
	return actor, ok
}

type Config struct {
	Service  string
	Verifier Verifier
	Cache    *Cache
	Rules    Rules
	Record   Recorder
	Logger   *slog.Logger
}

var (
	errUnmapped  = errors.New("procedure has no permission")
	errForbidden = errors.New("you do not have permission for this action")
	errLookup    = errors.New("permissions are unavailable")
)

func NewInterceptor(cfg Config) connect.UnaryInterceptorFunc {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if req.Spec().IsClient {
				return next(ctx, req)
			}
			raw, err := authn.BearerToken(req.Header())
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("no access token"))
			}
			claims, err := cfg.Verifier.Verify(raw)
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("access token is not valid"))
			}
			actor := Actor{
				UserID:    claims.UserID,
				ClientIP:  req.Header().Get(authn.ClientIPHeader),
				UserAgent: req.Header().Get("User-Agent"),
			}
			procedure := req.Spec().Procedure
			rule, mapped := cfg.Rules[procedure]
			if !mapped {
				log.ErrorContext(ctx, "admin procedure without a permission rule", "procedure", procedure)
				return nil, connect.NewError(connect.CodePermissionDenied, errUnmapped)
			}
			set, err := cfg.Cache.Permissions(ctx, claims.UserID, req.Header().Get(authn.Header))
			if err != nil {
				log.WarnContext(ctx, "could not load permissions", "user_id", claims.UserID, "error", err)
				return nil, connect.NewError(connect.CodeUnavailable, errLookup)
			}
			actor.Permissions = set
			ctx = WithActor(authn.WithClaims(ctx, claims), actor)
			if !set.Has(rule) {
				cfg.denied(ctx, log, procedure, rule)
				return nil, connect.NewError(connect.CodePermissionDenied, errForbidden)
			}
			return next(ctx, req)
		}
	}
}

func (cfg Config) denied(ctx context.Context, log *slog.Logger, procedure string, rule Permission) {
	if cfg.Record == nil {
		return
	}
	action := NewAction(ctx, cfg.Service, "permission_denied", "procedure", procedure, "")
	action.Outcome = Denied
	action.Details = map[string]string{"permission": rule.String()}
	if err := cfg.Record(context.WithoutCancel(ctx), action); err != nil {
		log.ErrorContext(ctx, "could not record a denied admin call", "procedure", procedure, "error", err)
	}
}
