package kafka

import (
	"fmt"
	"strings"
)

// separator splits a topic into its two halves.
const separator = "."

// maxSegment bounds each half. Kafka's own limit is 249 characters for the
// whole name; the shorter bound here is about readability, since a domain or an
// event that needs more than this is a naming problem, not a length problem.
const maxSegment = 64

// Topic is an event-bus topic spelled <domain>.<event>, for example
// "chat.message" or "game.started".
//
// It is a string type rather than a bare string so the contract is checked once,
// where the topic is built, instead of being re-read from rules/infra.md by
// everyone who publishes.
type Topic string

// NewTopic joins a domain and an event into a topic.
func NewTopic(domain, event string) (Topic, error) {
	return ParseTopic(domain + separator + event)
}

// ParseTopic validates an already-joined topic name.
func ParseTopic(s string) (Topic, error) {
	t := Topic(s)
	if err := t.Validate(); err != nil {
		return "", err
	}
	return t, nil
}

// Validate reports whether the topic matches the contract: exactly two
// segments, lowercase, separated by a single dot.
func (t Topic) Validate() error {
	domain, event, found := strings.Cut(string(t), separator)
	if !found || !validSegment(domain) || !validSegment(event) {
		return fmt.Errorf("kafka: topic %q: %w", string(t), ErrInvalidTopic)
	}
	return nil
}

// Domain is the part before the dot, empty for an invalid topic.
func (t Topic) Domain() string {
	domain, _, found := strings.Cut(string(t), separator)
	if !found {
		return ""
	}
	return domain
}

// Event is the part after the dot, empty for an invalid topic.
func (t Topic) Event() string {
	_, event, found := strings.Cut(string(t), separator)
	if !found {
		return ""
	}
	return event
}

// String returns the topic as it goes on the wire.
func (t Topic) String() string { return string(t) }

// validSegment accepts lowercase letters and digits, with hyphens allowed
// inside. Uppercase is rejected on purpose: Kafka topic names are
// case-sensitive, so "Chat.Message" and "chat.message" would be two different
// topics that look like one in a dashboard.
func validSegment(s string) bool {
	if s == "" || len(s) > maxSegment {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i > 0 && i < len(s)-1:
		default:
			return false
		}
	}
	return true
}

// validateTopics checks a whole subscription list and reports the first
// offender by name, so a misconfigured consumer says which topic is wrong.
func validateTopics(topics []Topic) error {
	if len(topics) == 0 {
		return fmt.Errorf("kafka: %w", ErrNoTopics)
	}
	for _, t := range topics {
		if err := t.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// topicNames converts a subscription list for the underlying client.
func topicNames(topics []Topic) []string {
	names := make([]string, len(topics))
	for i, t := range topics {
		names[i] = string(t)
	}
	return names
}
