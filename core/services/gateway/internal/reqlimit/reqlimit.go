package reqlimit

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func Body(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.MaxBytesHandler(next, maxBytes)
	}
}

func Deadline(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(timeout))
			next.ServeHTTP(w, r)
		})
	}
}

func Route(limit func(http.Handler) http.Handler) func(string, http.Handler) (string, http.Handler) {
	return func(path string, handler http.Handler) (string, http.Handler) {
		return path, limit(handler)
	}
}

type lists struct{ limit int }

func Lists(limit int) connect.Interceptor {
	return lists{limit: limit}
}

func (l lists) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if !req.Spec().IsClient {
			if msg, ok := req.Any().(proto.Message); ok {
				if err := CheckLists(msg, l.limit); err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, err)
				}
			}
		}
		return next(ctx, req)
	}
}

func (l lists) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (l lists) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(ctx, &checkedConn{StreamingHandlerConn: conn, limit: l.limit})
	}
}

type checkedConn struct {
	connect.StreamingHandlerConn
	limit int
}

func (c *checkedConn) Receive(msg any) error {
	if err := c.StreamingHandlerConn.Receive(msg); err != nil {
		return err
	}
	if m, ok := msg.(proto.Message); ok {
		if err := CheckLists(m, c.limit); err != nil {
			return connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	return nil
}

func CheckLists(msg proto.Message, limit int) error {
	if limit <= 0 {
		return nil
	}
	return walk(msg.ProtoReflect(), limit)
}

func walk(msg protoreflect.Message, limit int) error {
	var err error
	msg.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case field.IsList():
			list := value.List()
			if list.Len() > limit {
				err = fmt.Errorf("%s has %d items, at most %d are allowed", field.Name(), list.Len(), limit)
				return false
			}
			if field.Message() != nil {
				for i := range list.Len() {
					if err = walk(list.Get(i).Message(), limit); err != nil {
						return false
					}
				}
			}
		case field.IsMap():
			entries := value.Map()
			if entries.Len() > limit {
				err = fmt.Errorf("%s has %d entries, at most %d are allowed", field.Name(), entries.Len(), limit)
				return false
			}
			if field.MapValue().Message() != nil {
				entries.Range(func(_ protoreflect.MapKey, entry protoreflect.Value) bool {
					err = walk(entry.Message(), limit)
					return err == nil
				})
			}
		case field.Message() != nil:
			err = walk(value.Message(), limit)
		}
		return err == nil
	})
	return err
}
