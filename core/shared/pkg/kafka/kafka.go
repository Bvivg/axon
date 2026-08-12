package kafka

import "errors"

var (
	ErrInvalidTopic = errors.New("topic must be <domain>.<event>")

	ErrNoBrokers = errors.New("at least one broker address is required")

	ErrNoService = errors.New("service name is required")

	ErrNoTopics = errors.New("at least one topic is required")

	ErrNoHandler = errors.New("handler is required")
)
