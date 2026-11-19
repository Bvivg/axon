# Upload preflight

The web client hashes the exact bytes it will upload, after image preparation.
SHA-256 runs in a worker in 2 MiB chunks, with progress and cancellation. It sends
the hexadecimal digest as optional `source_sha256` in `POST /api/uploads`.

On a hit, the server returns the usual session response with `status: "ready"`
and `upload`. The client sends neither parts nor a completion request. Each hit
creates a separate upload draft pointing at the existing stored file; withdrawing
that draft leaves the original message and object intact. The attachment payload
keeps the newly selected filename.

On a miss, multipart upload proceeds normally. After assembly, the server checks
the actual SHA-256 against the claim before processing or remembering its source.
A mismatch ends the session with `failure: "hash_mismatch"`. Requests without a
digest continue to use the multipart path.

Reuse is scoped to upload mode and verified source size. An authenticated user
must own an unbound draft or have access to a live message containing the file,
including albums. Deleted messages, cleared history and rooms the user has left
do not grant access. The lookup locks the stored file and creates the draft and
ready session in one transaction, protecting them from garbage collection.

Existing media source hashes remain reusable. Their original source size can be
unknown; those legacy entries still require the same mode, hash and access check.
Documents uploaded before this change have no shared-file record and enter the
catalogue when their bytes are next uploaded. No bulk rewrite of old messages is
performed.

## Rollout

Apply migration `000011_upload_preflight` before starting the new chat service:

```sh
make migrate-up S=chat
docker compose -f core/deploy/docker-compose.yml up -d --build --renew-anon-volumes chat web
```

The web image needs the updated lockfile, including `@noble/hashes` 2.4.0. The
anonymous-volume renewal refreshes the development web container's dependencies;
it does not remove named database or object-storage volumes.

## Verification

- `internal/uploads/manager_test.go`: ready preflight, fresh upload IDs, mode and
  ownership isolation, invalid claims, shared-file withdrawal.
- `integration/uploads_test.go`: real Postgres/MinIO, recipients, outsiders,
  deleted and cleared messages, albums, removed members, concurrent reuse.
- `hash-blob.test.ts` and `hash-file.test.ts`: multi-chunk SHA-256, progress and
  worker termination on cancellation.
- `e2e/upload-preflight.spec.ts`: actual worker, no byte requests on repeat,
  filename and URL reuse, withdrawal, recipient access, bad hashes.
