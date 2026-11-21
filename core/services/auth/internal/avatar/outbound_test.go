package avatar

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/safehttp"
)

func TestDefaultPipelineRejectsPrivateAvatarSources(t *testing.T) {
	pipeline := NewPipeline(nil, NewURLBuilder("https://cdn.example.com", "avatars"), nil, DefaultLimits())
	for _, source := range []string{"http://127.0.0.1/avatar.png", "https://127.0.0.1/avatar.png", "https://169.254.169.254/avatar.png", "https://minio/avatar.png"} {
		t.Run(source, func(t *testing.T) {
			_, err := pipeline.ProcessFromURL(t.Context(), uuid.New(), source)
			if !errors.Is(err, safehttp.ErrUnsafeDestination) {
				t.Fatalf("expected unsafe destination, got %v", err)
			}
		})
	}
}
