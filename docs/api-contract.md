# API Contract

`api/openapi.yaml` is the source of truth for REST endpoints. Generated Go types live under `internal/types` and must not be hand-edited. The CLI and external agents consume this contract instead of linking to backend packages.

## Endpoint Groups

- `/healthz`: unauthenticated daemon reachability probe for local scripts and CI smoke checks.
- `/auth/*`: passphrase login, refresh-token rotation, and logout. These endpoints will be rate-limited in A1.
- `/session/*`: WhatsApp QR login, status, and logout. Status is intentionally unauthenticated so CLI tooling can tell whether the daemon is alive before login.
- `/chats` and `/chats/{jid}/messages`: read-only data access. Once encrypted storage is locked these return `423 Locked`.
- `/messages` and `/messages/{id}/*`: send, react, reply, and forward actions. Every write is audited with an actor.
- `/contacts` and `/contacts/{jid}`: contact listing and alias management.
- `/groups/{jid}` and `/groups/{jid}/participants`: group metadata and participant resolution.
- `/media/*`: upload/download media blobs. Storage is content-addressed by SHA256 on disk.

## A3 Media Sending

`POST /messages` accepts the existing text shape and, for CLI-local media, either `file_path` or `audio_path` plus optional `caption`.

- `file_path` sends images as WhatsApp image messages when the MIME type starts with `image/`; other files are sent as documents.
- `audio_path` sends the file with the WhatsApp audio envelope and stores it as an `audio` message.
- The daemon stores a deduplicated local copy under its media directory before upload, keyed by SHA256.

## A4 Message Actions

`POST /messages/{id}/react` sends an emoji reaction to a message already present in the local store. The daemon uses the stored `chat_jid` and `sender_jid` to build the WhatsApp message key, persists the local user's reaction in `messages.reactions_json`, emits `message.reaction`, and returns `204`.

`POST /messages/{id}/reply` sends a text reply quoting the stored message. The reply is persisted as a normal text message with `reply_to_id` set to the quoted message ID, then the daemon emits the same `message.new` and `chat.updated` events used by `POST /messages`.

`POST /messages/{id}/forward` currently forwards stored text messages to one or more target chats. Each accepted forward is persisted as a new outgoing message and returned in an `items` array. Media forwarding will require preserving enough WhatsApp media metadata to re-send without reading only the local file path, so it remains outside this A4 slice.

Message responses expose a nullable `status` field for delivery indicators. The store keeps WhatsApp-facing states such as `server_ack`, `retry`, and `server_error`; the HTTP API normalizes them for clients as `sent`, `failed`, `delivered`, `read`, `pending`, or `received`.

## A5 Contacts And Groups

`GET /contacts` returns known contacts with `display_name` resolved as `alias > agenda_name > push_name > formatted jid`. `PATCH /contacts/{jid}` sets or clears a user alias. Aliases are the highest-priority name source and immediately affect chat/group participant output. `agenda_name` is sourced from WhatsApp contact sync data when available, while `push_name` is the sender profile name seen in messages.

`GET /groups` refreshes joined-group metadata from WhatsApp when the session provider is available, stores it locally, and returns the local group list. `GET /groups/{jid}` refreshes one group. `GET /groups/{jid}/participants` refreshes and returns participants with resolved display names and roles.

The CLI surface is:

- `wh contacts`
- `wh contact alias <jid> <alias>`
- `wh groups`
- `wh group participants <group_jid>`

## Error Shape

Handlers currently return compact JSON errors as `{"error":"code"}`. The important auth status codes are already active: `401 Unauthorized` for invalid credentials/tokens and `503 Service Unavailable` when a daemon dependency is not configured.

## WebSocket

`api/asyncapi.yaml` documents the `/ws` event channel. Events are typed JSON records with `id`, `type`, `ts`, optional `chat_jid`, and an event-specific `payload`.
