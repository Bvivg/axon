module github.com/bvivg/axon/core/services/auth

go 1.26.0

// shared is a sibling directory in this repository, never a published version.
// Without this, `go mod tidy` resolves it from GitHub and pins whatever commit
// happens to be pushed — which goes stale immediately and makes an image build
// fetch code that is already in the build context. go.work points at the same
// directory, so this matters only when the module is built on its own.
replace github.com/bvivg/axon/core/shared => ../../shared

require (
	connectrpc.com/connect v1.20.0
	github.com/bvivg/axon/core/shared v0.0.0-00010101000000-000000000000
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.10.0
	golang.org/x/crypto v0.54.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.24.1 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.1 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)
