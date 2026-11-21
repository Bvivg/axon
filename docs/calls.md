# Calls

Audio and video calls between chat users: 1:1 calls peer to peer, group calls on
a self-hosted SFU, and an automatic switch from P2P to the SFU when an active 1:1
call degrades. The detailed plan, gates and results are in
[`calls-hybrid.md`](../.claude/plans/calls-hybrid.md) (Russian).

## ADR-1: Calls live inside Chat

Calls are a module of the Chat service, not a separate service. Call state lives
in the `chat` schema (`chat.call_*` tables) and the API is its own contract,
`axon.calls.v1`. A call starts from a chat or from a person, reuses chat
membership for access control, and is delivered over the chat WebSocket, so a
separate service would only add a network hop and a second copy of membership.
The extraction boundary is kept explicit: the `axon.calls.v1` contract, the Call
ID, and the Store, Signaling and MediaProvider interfaces. Moving Calls into its
own service later changes those adapters, not the clients. The empty `calling`
schema stays reserved for that.

## Shape of the design

- **Source of truth.** Postgres holds call state, versions, participants, busy
  slots, idempotency and the outbox, all changed in one transaction. Redis only
  delivers signaling and presence. Kafka carries business events such as
  `call.ended.v1`, never SDP or ICE.
- **Call lifecycle.** `CREATED → RINGING → CONNECTING → ACTIVE → ENDED`; cancel,
  decline, busy, timeout and errors are terminal outcomes. One endpoint wins an
  accept and the others get `answered_elsewhere`. A user cannot be busy in two
  calls at once.
- **1:1 is native WebRTC.** Browsers connect directly or through coturn, with
  short-lived TURN credentials issued only to authorized endpoints. Media is
  DTLS-SRTP and never passes through the gateway.
- **Groups use LiveKit.** A self-hosted LiveKit node behind the
  `docker-compose.calls-sfu.yml` overlay. The gateway proxies LiveKit signaling
  and Chat checks every join: token signature, call, endpoint, generation,
  membership and the user's session family. There are no mesh groups.
- **Adding a third person** starts a new group call linked to the finished 1:1
  call, and the interface shows the reconnect.
- **P2P to SFU migration.** When an active 1:1 connection fails, the client
  requests a migration. The SFU path is prepared in parallel with its audio
  muted, both clients switch, the server commits the new transport under the
  same Call ID, and the old peer connection is cleaned up. If anything fails
  before the commit, the call rolls back to P2P or ends. Operation ids,
  transport serials and generations fence late messages.
- **Cleanup is part of the contract.** Ending a call stops capture, remote
  tracks, screen share and playback before the client acknowledges.

## Verification

Calls are tested with real media in Docker rather than mocks: Playwright drives
Chromium with fake capture devices and checks RTP counters, audio energy, video
frames, the selected ICE pair and track cleanup. CI runs three suites:

```sh
bash core/deploy/calls-e2e.sh             # 1:1 RPC, WebSocket and UI media gates
bash core/deploy/calls-sfu-e2e.sh         # groups, fallback and cleanup on LiveKit
bash core/deploy/calls-migration-e2e.sh   # P2P to SFU migration and cleanup
```

Not verified: an external NAT outside the local network and push notifications
for incoming calls. Safari on iOS has only a partial manual check; the checklist
is in [`calls-safari-ios-runbook.md`](calls-safari-ios-runbook.md).

## LAN stand

```sh
LAN_IP=192.168.2.1 LAN_SECRETS_DIR=.local/evidence/calls/LAN/.secrets node core/deploy/calls-lan-init.mjs
AXON_LAN_IP=192.168.2.1 docker compose -f core/deploy/docker-compose.yml -f core/deploy/docker-compose.calls-lan.yml up -d
```

- The generator keeps the CA, the TURN secret and a random MinIO root password in `minio-root.env`. It reissues only the server certificate. The overlay refuses to start without `minio-root.env`.
- Auth and Chat reach MinIO as their own users, `axon-avatars` and `axon-attachments`. Each user has a policy for its own bucket only (`core/deploy/minio`). The root account stays with `minio` and `minio-init`.
- Under `/storage/` the proxy passes only `GET` and `HEAD` for public avatars (`/storage/avatars/<uuid>/<size>.jpg`), with no query string and no `Authorization`. The MinIO S3 and admin APIs are not reachable from outside.
- HTTPS (443, 3443), the CA (3444) and coturn bind only to `AXON_LAN_IP`, and HTTPS also to 127.0.0.1. To reach the stand at another address, for example over Tailscale, add the binding in a local overlay.
