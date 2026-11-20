package calls

import "context"

type MediaJobs interface {
	RecordWebhook(context.Context, string, []byte, string, string) error
	MediaRoomEnded(context.Context, string) (bool, error)
	ObserveMediaRoom(context.Context, string, string) error
}
