package calls

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

type MediaProvider struct {
	Rooms                        *lksdk.RoomServiceClient
	APIKey, APISecret, PublicURL string
}

func (m *MediaProvider) Available(ctx context.Context) error {
	timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := m.Rooms.ListRooms(timeout, &livekit.ListRoomsRequest{}); err != nil {
		return ErrUnavailable
	}
	return nil
}
func (m *MediaProvider) Issue(ctx context.Context, call Call, p Participant) (string, time.Time, error) {
	timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	room := "call_" + call.ID.String()
	if _, err := m.Rooms.CreateRoom(timeout, &livekit.CreateRoomRequest{Name: room, EmptyTimeout: 30, DepartureTimeout: 2, MaxParticipants: MaxParticipants}); err != nil {
		return "", time.Time{}, ErrUnavailable
	}
	identity := p.EndpointID.String() + ":" + strconv.FormatInt(p.Epoch, 10) + ":" + strconv.FormatInt(call.Generation, 10) + ":" + strconv.FormatInt(p.MediaVersion, 10)
	participants, err := m.Rooms.ListParticipants(timeout, &livekit.ListParticipantsRequest{Room: room})
	if err != nil {
		return "", time.Time{}, ErrUnavailable
	}
	for _, old := range participants.Participants {
		prefix := p.EndpointID.String() + ":"
		if len(old.Identity) >= len(prefix) && old.Identity[:len(prefix)] == prefix && old.Identity != identity {
			if _, err := m.Rooms.RemoveParticipant(timeout, &livekit.RoomParticipantIdentity{Room: room, Identity: old.Identity}); err != nil {
				return "", time.Time{}, ErrUnavailable
			}
		}
	}
	allow, deny := true, false
	sources := []string{"microphone"}
	if call.Mode == "video" {
		sources = []string{"microphone", "camera", "screen_share", "screen_share_audio"}
	}
	expires := time.Now().UTC().Add(2 * time.Minute)
	grant := &auth.VideoGrant{RoomJoin: true, Room: room, CanPublish: &allow, CanSubscribe: &allow, CanPublishData: &deny, CanUpdateOwnMetadata: &deny, CanPublishSources: sources}
	token, err := auth.NewAccessToken(m.APIKey, m.APISecret).SetIdentity(identity).SetVideoGrant(grant).SetValidFor(2 * time.Minute).ToJWT()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("calls: sign media token: %w", err)
	}
	return token, expires, nil
}
