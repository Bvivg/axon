package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/service"
)

type auditedStore struct {
	*fakeStore
	asked []domain.AdminUploadFilter
}

func (s *auditedStore) AdminUploads(_ context.Context, f domain.AdminUploadFilter) ([]domain.AdminUpload, string, error) {
	s.asked = append(s.asked, f)
	return []domain.AdminUpload{{ID: uuid.New(), Kind: f.Kind}}, "next", nil
}

func TestAdminUploadsChecksTheKindAndBoundsThePage(t *testing.T) {
	tests := map[string]struct {
		filter domain.AdminUploadFilter
		limit  int
		field  string
	}{
		"no page size":        {filter: domain.AdminUploadFilter{}, limit: 25},
		"a page too large":    {filter: domain.AdminUploadFilter{Limit: 500}, limit: 25},
		"a negative page":     {filter: domain.AdminUploadFilter{Limit: -1}, limit: 25},
		"a page within range": {filter: domain.AdminUploadFilter{Limit: 10, Kind: domain.MessageKindImage}, limit: 10},
		"every upload kind":   {filter: domain.AdminUploadFilter{Limit: 100, Kind: domain.MessageKindVoice}, limit: 100},
		"an album":            {filter: domain.AdminUploadFilter{Kind: domain.MessageKindAlbum}, field: "kind"},
		"a text message":      {filter: domain.AdminUploadFilter{Kind: domain.MessageKindText}, field: "kind"},
		"an unknown kind":     {filter: domain.AdminUploadFilter{Kind: "spreadsheet"}, field: "kind"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			store := &auditedStore{fakeStore: newFakeStore()}
			svc, err := service.New(service.Config{Store: store, Events: &recordingEvents{}, Logger: logger.Discard()})
			if err != nil {
				t.Fatalf("service.New: %v", err)
			}
			uploads, next, err := svc.AdminUploads(t.Context(), tt.filter)
			if tt.field != "" {
				v, ok := domain.AsValidationError(err)
				if !ok || v.Field != tt.field {
					t.Fatalf("err = %v, want a validation error on %s", err, tt.field)
				}
				if len(store.asked) != 0 {
					t.Errorf("the store was asked %v for a refused filter", store.asked)
				}
				return
			}
			if err != nil || len(uploads) != 1 || next != "next" {
				t.Fatalf("AdminUploads = %v, %q, %v", uploads, next, err)
			}
			if len(store.asked) != 1 || store.asked[0].Limit != tt.limit || store.asked[0].Kind != tt.filter.Kind {
				t.Errorf("store asked %+v, want limit %d and kind %q", store.asked, tt.limit, tt.filter.Kind)
			}
		})
	}
}

func TestAdminUploadsNeedAStoreThatCanListThem(t *testing.T) {
	h := newHarness(t)
	if _, _, err := h.svc.AdminUploads(t.Context(), domain.AdminUploadFilter{}); !errors.Is(err, service.ErrModerationUnavailable) {
		t.Fatalf("err = %v, want ErrModerationUnavailable", err)
	}
}
