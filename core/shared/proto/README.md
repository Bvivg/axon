# Contracts

Every service contract lives here as `axon/<service>/v1/<service>.proto` and is
the single source of truth for Go, TypeScript and (later) Swift types. Nothing
that crosses a service boundary is hand-written on either side.

## Conventions

- One versioned package per service: `axon.auth.v1`, `axon.chat.v1`. The
  directory has to mirror the package exactly — `buf lint` enforces it, which is
  why the tree starts with an `axon/` level.
- Enum names avoid consecutive capitals (`OauthProvider`, not `OAuthProvider`):
  buf derives the required value prefix from the type name, and `OAuthProvider`
  would demand values spelled `O_AUTH_PROVIDER_*`.
- Methods are verb + noun: `Register`, `RefreshToken`, `ListActiveGames`.
- New fields are added as `optional` and never renumbered; a change that cannot
  be made additively ships as `v2` alongside `v1`.
- Realtime traffic — chat messages, game moves — goes over WebSocket, not over
  unary RPC. Only request/response operations belong in these files.

## Generating

Generation runs in a container; `buf` is not installed on the host.

```sh
make proto
```

Output lands in `core/shared/gen/go` and `ui/web/src/gen`, both git-ignored and
regenerated on demand.

This directory holds no contracts yet — the first one (`auth/v1/auth.proto`)
arrives with the auth service. The `buf` toolchain, `buf.yaml` and
`buf.gen.yaml` at the repository root are already wired, so `make proto` starts
producing output the moment a `.proto` file lands here.
