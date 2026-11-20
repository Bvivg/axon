module github.com/bvivg/axon/core/services/gateway

go 1.27.4

// shared is a sibling directory in this repository, never a published version.
// go.work points at the same directory, so this matters only when the module is
// built on its own — which is what the service image does.
replace github.com/bvivg/axon/core/shared => ../../shared

require (
	connectrpc.com/connect v1.21.1
	github.com/bvivg/axon/core/shared v0.0.0-20261001044949-82e8b07ea469
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	golang.org/x/time v0.16.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pandatix/go-abnf v0.5.5 // indirect
	github.com/prometheus/client_golang v1.25.0 // indirect
	github.com/prometheus/client_model v0.6.3 // indirect
	github.com/prometheus/common v0.73.1 // indirect
	github.com/prometheus/procfs v0.23.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)
