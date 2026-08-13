package kafka

import (
	"fmt"
	"strings"
)

const separator = "."

const maxSegment = 64

type Topic string

func NewTopic(domain, event string) (Topic, error) {
	return ParseTopic(domain + separator + event)
}

func ParseTopic(s string) (Topic, error) {
	t := Topic(s)
	if err := t.Validate(); err != nil {
		return "", err
	}
	return t, nil
}

func (t Topic) Validate() error {
	domain, event, found := strings.Cut(string(t), separator)
	if !found || !validSegment(domain) || !validSegment(event) {
		return fmt.Errorf("kafka: topic %q: %w", string(t), ErrInvalidTopic)
	}
	return nil
}

func (t Topic) Domain() string {
	domain, _, found := strings.Cut(string(t), separator)
	if !found {
		return ""
	}
	return domain
}

func (t Topic) Event() string {
	_, event, found := strings.Cut(string(t), separator)
	if !found {
		return ""
	}
	return event
}

func (t Topic) String() string { return string(t) }

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

func topicNames(topics []Topic) []string {
	names := make([]string, len(topics))
	for i, t := range topics {
		names[i] = string(t)
	}
	return names
}
