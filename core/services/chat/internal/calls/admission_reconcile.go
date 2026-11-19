package calls

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/webhook"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

type MediaReconciler struct {
	Jobs      MediaJobs
	Admission *MediaAdmission
	Rooms     *lksdk.RoomServiceClient
	Log       *slog.Logger
}

func (m *MediaReconciler) room(ctx context.Context, name string) error {
	if !strings.HasPrefix(name, "call_") {
		return nil
	}
	if m.Jobs != nil {
		ended, err := m.Jobs.MediaRoomEnded(ctx, name)
		if err != nil {
			return err
		}
		if ended {
			_, err := m.Rooms.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: name})
			if err == nil {
				m.Log.InfoContext(ctx, "media room removed after End", "room", name)
			}
			return err
		}
	}
	participants, err := m.Rooms.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: name})
	if err != nil {
		return err
	}
	for _, participant := range participants.Participants {
		token, err := auth.NewAccessToken(m.Admission.APIKey, m.Admission.APISecret).SetIdentity(participant.Identity).SetVideoGrant(&auth.VideoGrant{RoomJoin: true, Room: name}).SetValidFor(time.Minute).ToJWT()
		if err != nil {
			return err
		}
		if _, err = m.Admission.Check(ctx, token); err == nil {
			continue
		}
		if _, err = m.Rooms.RemoveParticipant(ctx, &livekit.RoomParticipantIdentity{Room: name, Identity: participant.Identity}); err != nil {
			return err
		}
		m.Log.InfoContext(ctx, "media participant removed after admission revoke", "room", name, "identity", participant.Identity)
	}
	return nil
}

func (m *MediaReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
			rooms, err := m.Rooms.ListRooms(timeout, &livekit.ListRoomsRequest{})
			if err == nil {
				for _, room := range rooms.Rooms {
					if m.Jobs != nil && strings.HasPrefix(room.Name, "call_") {
						if err = m.Jobs.ObserveMediaRoom(timeout, room.Name, room.Sid); err != nil {
							break
						}
					}
					if err = m.room(timeout, room.Name); err != nil {
						break
					}
				}
			}
			cancel()
			if err != nil && ctx.Err() == nil {
				m.Log.WarnContext(ctx, "media reconciliation unavailable")
			}
		}
	}
}

func (m *MediaReconciler) Webhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid webhook", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	event, err := webhook.ReceiveWebhookEvent(r, auth.NewSimpleKeyProvider(m.Admission.APIKey, m.Admission.APISecret))
	if err != nil {
		http.Error(w, "invalid webhook", http.StatusUnauthorized)
		return
	}
	if m.Jobs != nil && event.Room != nil && strings.HasPrefix(event.Room.Name, "call_") {
		hash := sha256.Sum256(body)
		if err := m.Jobs.RecordWebhook(r.Context(), event.Id, hash[:], event.Room.Name, event.Event); err != nil {
			http.Error(w, "webhook receipt rejected", http.StatusConflict)
			return
		}
	}
	if event.Room != nil && (event.Event == "participant_joined" || event.Event == "participant_connection_aborted") {
		m.Log.InfoContext(r.Context(), "media webhook verified", "event", event.Event, "event_id", event.Id, "room", event.Room.Name)
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := m.room(ctx, event.Room.Name); err != nil {
			http.Error(w, "reconciliation unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}
