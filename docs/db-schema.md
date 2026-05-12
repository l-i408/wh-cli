# Database Schema

The persistent store is a single SQLite database opened through SQLCipher once A1 lands. A0 creates the first migration with the minimum tables required by the project plan.

## Tables

- `device_session`: encrypted WhatsApp multi-device session blob. A single row is enforced with `id = 1`.
- `contacts`: contact names from PushName, local address book, manual alias, and avatar path. Alias is user-controlled; PushName is refreshed from WhatsApp events.
- `groups`: group metadata, owner, topic, and timestamps.
- `group_participants`: many-to-many relation between groups and contacts with participant role.
- `chats`: chat list state, unread count, pin/mute metadata, and last message pointer.
- `messages`: normalized message records for text, image, document, and audio messages.
- `media_blobs`: deduplicated media metadata keyed by SHA256 and local path. A sent media row is first created from the local file hash and then updated with the WhatsApp message id once the send succeeds.
- `jid_mappings`: LID-to-phone-number mappings copied from whatsmeow's session store. The API uses this table to canonicalize duplicate `@s.whatsapp.net` chats to their `@lid` thread.
- `auth_tokens`: access and refresh token `jti` registry for revocation and refresh rotation.
- `audit_log`: append-only record of write actions with actor, target, result, and severity.

## Indexes

- `idx_messages_chat_timestamp` supports paginated chat history.
- `idx_auth_tokens_expires` supports cleanup and refresh-token validation.
- `idx_audit_log_ts` supports chronological audit review.

## Media Storage

Local outbound media is copied into the daemon media directory as `<sha256><original_ext>` with owner-only permissions. `media_blobs.sha256` is unique, which makes repeated sends of the same payload reuse one stored file while allowing the latest `message_id` to point at the WhatsApp message that used it.

## Naming Rule

Display names are not persisted as a separate source of truth. Repositories should compute them as `alias > agenda_name > push_name > formatted jid`, and presentation layers should avoid raw JIDs where a better name exists. `agenda_name` is populated from WhatsApp contact sync data when available; `push_name` remains the fallback profile name from message metadata.
