# Security

`wh-cli` is designed as a local automation daemon. The default trust boundary is the local machine, and remote access requires an explicit future TLS path.

## Defaults

- Bind to `127.0.0.1` by default.
- Reject non-local listen addresses until the TLS opt-in flow exists.
- Store WhatsApp session and application data in SQLCipher.
- Store the master key and JWT secret in the OS keyring when available.
- Fall back to passphrase unlock with Argon2id.

## Key Management

The master key is 32 bytes. In the default mode it is stored via the OS keyring under service `wh-cli`, account `master-key`. If keyring access fails, the daemon prompts for a passphrase and derives the key with Argon2id using `time=3`, `memory=64MB`, `threads=4`, and a salt stored under the user config directory.

The JWT signing secret is separately stored in keyring account `jwt-secret`. A1 implements short access tokens, refresh tokens, `jti` persistence, and refresh rotation. CLI login stores access and refresh tokens in the OS keyring under public account labels; token values are not passed through logout flags.

## Logging

The backend uses `log/slog`. Code must never log message bodies, tokens, passphrases, or full JIDs. Use `internal/logutil.RedactJID` when a JID-shaped value must appear in diagnostics.

## Repository Hygiene

Pre-commit blocks database files, `session/` content, and real WhatsApp JIDs matching `\d{8,}@s\.whatsapp\.net`. Secret scanning uses `gitleaks` and `detect-secrets`.
