// Package kafka is Axon's event-bus transport: a producer and a consumer over
// the topic contract from rules/infra.md.
//
// The package moves bytes and metadata. It has no opinion about what is inside
// a message — encoding belongs to the domain that publishes it — and no opinion
// about what a consumer does when handling fails, because retries, dead-letter
// queues and backoff are policy, and policy belongs to the service that owns
// the consumer.
//
// Two things it does insist on, because both are rules rather than preferences:
//
//   - Topics are <domain>.<event>. The Topic type refuses anything else, so a
//     typo fails at the call site rather than creating a stray topic on a broker
//     with auto-creation enabled.
//   - A consumer group is named after the consuming service. The config field is
//     the service name and the group id is derived from it, so the "one group per
//     consuming service" rule cannot be broken by editing a string.
//
// Correlation IDs ride along in message headers (see shared/pkg/correlation):
// the producer writes the ID from its context, the consumer lifts it back into
// the context it hands the handler. Without that, a trace ends at the bus.
package kafka

import "errors"

// Configuration and contract errors. They are sentinels so callers can react to
// a specific problem instead of matching on message text.
var (
	// ErrInvalidTopic reports a topic that is not <domain>.<event>.
	ErrInvalidTopic = errors.New("topic must be <domain>.<event>")
	// ErrNoBrokers reports an empty broker list.
	ErrNoBrokers = errors.New("at least one broker address is required")
	// ErrNoService reports a missing service name. The producer stamps it on
	// its logs; the consumer turns it into the group id.
	ErrNoService = errors.New("service name is required")
	// ErrNoTopics reports a consumer with nothing to subscribe to.
	ErrNoTopics = errors.New("at least one topic is required")
	// ErrNoHandler reports Run called without a handler.
	ErrNoHandler = errors.New("handler is required")
)
