package calls

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type MediaBinding struct {
	CallID, EndpointID uuid.UUID
	Epoch, Generation  int64
	MediaVersion       int64
	AllowP2PProbe      bool
	Room, Identity     string
}

type MediaStore interface {
	MediaEndpoint(context.Context, MediaBinding, time.Time) (Endpoint, error)
}

type MediaFamily interface {
	CheckMediaFamily(context.Context, uuid.UUID, uuid.UUID, string) error
}

type MediaAdmission struct {
	AllowP2PProbe                  bool
	Store                          MediaStore
	Families                       MediaFamily
	APIKey, APISecret, InternalKey string
}

func (a *MediaAdmission) Check(ctx context.Context, raw string) (MediaBinding, error) {
	var binding MediaBinding
	if len(raw) == 0 || len(raw) > 16384 || len(a.APISecret) < 32 {
		return binding, ErrForbidden
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(_ *jwt.Token) (any, error) { return []byte(a.APISecret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(a.APIKey), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return binding, ErrForbidden
	}
	video, ok := claims["video"].(map[string]any)
	if !ok || video["roomJoin"] != true || video["roomAdmin"] == true || video["roomCreate"] == true || video["roomList"] == true || video["roomRecord"] == true {
		return binding, ErrForbidden
	}
	room, ok := video["room"].(string)
	if !ok {
		return binding, ErrForbidden
	}
	subject, err := claims.GetSubject()
	if err != nil {
		return binding, ErrForbidden
	}
	parts := strings.Split(subject, ":")
	if (len(parts) != 4 && (len(parts) != 3 || !a.AllowP2PProbe)) || !strings.HasPrefix(room, "call_") {
		return binding, ErrForbidden
	}
	binding.CallID, err = uuid.Parse(strings.TrimPrefix(room, "call_"))
	if err != nil {
		return binding, ErrForbidden
	}
	binding.EndpointID, err = uuid.Parse(parts[0])
	if err != nil {
		return binding, ErrForbidden
	}
	binding.Epoch, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil || binding.Epoch < 1 {
		return binding, ErrForbidden
	}
	binding.Generation, err = strconv.ParseInt(parts[2], 10, 64)
	if err != nil || binding.Generation < 1 {
		return binding, ErrForbidden
	}
	if room != "call_"+binding.CallID.String() || parts[0] != binding.EndpointID.String() {
		return binding, ErrForbidden
	}
	if len(parts) == 4 {
		binding.MediaVersion, err = strconv.ParseInt(parts[3], 10, 64)
		if err != nil || binding.MediaVersion < 1 {
			return binding, ErrForbidden
		}
	}
	binding.AllowP2PProbe = a.AllowP2PProbe
	binding.Room, binding.Identity = room, subject
	endpoint, err := a.Store.MediaEndpoint(ctx, binding, time.Now().UTC())
	if err != nil {
		return binding, err
	}
	if err := a.Families.CheckMediaFamily(ctx, endpoint.UserID, endpoint.FamilyID, a.InternalKey); err != nil {
		return binding, fmt.Errorf("media family: %w", err)
	}
	return binding, nil
}
