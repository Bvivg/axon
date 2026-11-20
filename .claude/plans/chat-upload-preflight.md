# Upload preflight

Move duplicate detection ahead of byte transfer, following the existing flow in
the neighbouring nod project. Preserve the multipart contract for older clients.

## Implementation

- `ui/web/src/lib/chat/hash-blob.ts`, `hash.worker.ts`, `hash-file.ts`: streaming
  SHA-256 in a cancellable worker; hash the prepared blob rather than the original
  image before conversion.
- `upload.ts`, `use-pending-uploads.ts`, `message-composer.tsx`: send the digest at
  session creation, show Checking file, consume a ready upload without PUTs.
- Chat migration `000011`: persist the expected hash and known source size.
- `internal/uploads/manager.go`: attempt preflight first, verify every claimed
  hash on a miss, store documents in the same shared-file catalogue as media.
- `internal/repository/session.go`: ACL-aware reuse under a file lock, with a
  new upload/session per attachment. Preserve original references during cancel.
- `docs/chat-upload-preflight.md`: compatibility and rollout.

## Checks

Go unit tests for chat; upload integration tests with real Postgres and MinIO;
web TypeScript, ESLint and Vitest; production web build; Playwright preflight,
multipart and media scenarios. Inspect the live local stand after these pass.

Done when repeating an accessible file or photo produces a ready attachment
without any part requests, while unknown or inaccessible content still uploads
and a false digest cannot populate the catalogue.
