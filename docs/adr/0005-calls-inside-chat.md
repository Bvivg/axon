# 0005. Calls as a module of the chat service

- Status: accepted, 2026-11-03 (owner decision, ADR-1 in the calls plan)
- Code: [`core/services/chat`](../../core/services/chat) (Calls module, contract `axon.calls.v1`, tables `chat.call_*`)

## Context

The original architecture had a separate `calling` service: a thin LiveKit orchestrator with its own schema. When calls were actually designed, almost everything they need already belongs to chat:

- who may call whom is room membership, which chat owns;
- call signals ride the same realtime path as messages (Postgres transaction and outbox → Redis → WebSocket);
- call history is shown in the conversation.

A separate service would have to copy membership or ask chat for it on every call, and would add a deployable for a feature with a small number of concurrent users.

## Decision

- Calls are a module inside chat, in schema `chat`, with business tables prefixed `call_`. No new Go service and no `calling` module are created. The empty, reserved `calling` schema is kept and not used.
- The module has its own contract, `axon.calls.v1`, separate from the chat RPCs.
- Postgres is the source of truth for call state, versions, participants, busy slots, idempotency and outbox. Redis is delivery and presence only. Kafka carries optional business events (`call.ended.v1`), never SDP or ICE.
- 1:1 calls use native WebRTC, direct or through coturn. Groups use a self-hosted LiveKit SFU. Media goes from the client straight to LiveKit or coturn, never through gateway.
- The LiveKit SDK stays out of ordinary chat RPCs, pub/sub and message components.

**Extraction boundary.** If calls ever need independent scaling or lifecycle, they can be moved out without changing clients. The boundary is `CallsService` with its own `axon.calls.v1`, the narrow `Store`, `Signaling` and `MediaProvider` interfaces, and `call_`-prefixed tables. Chat owns membership and rooms; Calls reads them through an adapter. Extraction would keep the Call ID, the external RPC and the client model, and move data and ACL access to APIs or events rather than cross-schema joins.

## Alternatives rejected

- A separate `calling` service from the start: duplicates membership and realtime delivery for no measured need.
- Mesh groups of several P2P connections: groups go through the SFU.
- Migrating an active call between transports as part of the core scope: kept as an optional, separately approved stage.

## Consequences

- Calls add to chat's code size, and every chat release ships calls too.
- Revoking access cannot physically cut an already running P2P media path between two untrusted clients; lease and revoke stop well-behaved clients and refuse new signalling and TURN credentials.
- The documented limits are explicit: real Web Push delivery, external NATs and physical Safari/iOS devices were not verified.

## Evidence

- Decision table and boundary: [calls plan §1](../../.claude/plans/calls-hybrid.md#1-зафиксированные-решения-и-границы).
- Weak spots and unverified assumptions: [calls plan §11](../../.claude/plans/calls-hybrid.md#11-слабые-места-и-непроверенные-допущения).
- Calls e2e: [`core/deploy/calls-e2e.sh`](../../core/deploy/calls-e2e.sh), [`calls-sfu-e2e.sh`](../../core/deploy/calls-sfu-e2e.sh), [`calls-migration-e2e.sh`](../../core/deploy/calls-migration-e2e.sh); overlays in [core/deploy/README.md](../../core/deploy/README.md#calls-overlays).
