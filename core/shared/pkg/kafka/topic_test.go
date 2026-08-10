package kafka_test

import (
	"errors"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/kafka"
)

// The topic contract from rules/infra.md is <domain>.<event>. These are the
// spellings that have to be accepted and the ones that have to be refused
// before they reach a broker with auto-creation enabled, where a typo becomes a
// real topic that nobody consumes.
func TestParseTopic(t *testing.T) {
	valid := []string{
		"chat.message",
		"game.started",
		"game.finished",
		"call.ended",
		"game.session-created",
		"v2.event1",
	}
	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			topic, err := kafka.ParseTopic(name)
			if err != nil {
				t.Fatalf("ParseTopic(%q) = %v, want it accepted", name, err)
			}
			if topic.String() != name {
				t.Fatalf("round trip changed the topic: %q -> %q", name, topic)
			}
		})
	}

	invalid := map[string]string{
		"empty":              "",
		"no event":           "chat",
		"trailing dot":       "chat.",
		"leading dot":        ".message",
		"empty middle":       "chat..message",
		"three segments":     "chat.message.v2",
		"uppercase":          "Chat.Message",
		"underscore":         "chat_message",
		"space":              "chat message",
		"leading hyphen":     "-chat.message",
		"trailing hyphen":    "chat.message-",
		"slash":              "chat/message",
		"segment far too be": string(make([]byte, 300)),
	}
	for name, topic := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := kafka.ParseTopic(topic); !errors.Is(err, kafka.ErrInvalidTopic) {
				t.Fatalf("ParseTopic(%q) = %v, want ErrInvalidTopic", topic, err)
			}
		})
	}
}

func TestNewTopicJoinsSegments(t *testing.T) {
	topic, err := kafka.NewTopic("chat", "message")
	if err != nil {
		t.Fatalf("NewTopic: %v", err)
	}

	if topic.String() != "chat.message" {
		t.Fatalf("topic = %q, want chat.message", topic)
	}
	if topic.Domain() != "chat" {
		t.Fatalf("Domain = %q, want chat", topic.Domain())
	}
	if topic.Event() != "message" {
		t.Fatalf("Event = %q, want message", topic.Event())
	}
}

// A domain containing the separator would smuggle a third segment past
// NewTopic if the result were not validated as a whole.
func TestNewTopicRejectsSeparatorInSegments(t *testing.T) {
	if _, err := kafka.NewTopic("chat.private", "message"); !errors.Is(err, kafka.ErrInvalidTopic) {
		t.Fatalf("err = %v, want ErrInvalidTopic", err)
	}
}

func TestTopicPartsOfInvalidTopicAreEmpty(t *testing.T) {
	topic := kafka.Topic("nonsense")

	if topic.Domain() != "" || topic.Event() != "" {
		t.Fatalf("Domain/Event = %q/%q, want both empty", topic.Domain(), topic.Event())
	}
}
