# Production and GitHub release plan

## Public Scope

Ship:

- `wh-cli` binary.
- Local daemon on `127.0.0.1:7777`.
- Human CLI commands.
- JSON CLI mode for agents.
- REST API and WebSocket event stream.
- Secure local storage, keyring integration and encrypted export.

Do not ship:

- Bubble Tea TUI.
- Real WhatsApp screenshots, QR images, logs, local databases or session files.
- Local test account data.

## Repository Checklist

Before pushing to GitHub:

1. Run `go test -tags "sqlite_omit_load_extension" ./...`.
2. Run `go build -tags "sqlite_omit_load_extension" -o bin/wh-cli ./cmd/wh-cli`.
3. Run the runtime-state pre-commit script.
4. Check `git status --ignored` and verify ignored files are only local artifacts.
5. Check there are no real WhatsApp JIDs:

```powershell
rg "\d{8,}@s\.whatsapp\.net"
```

6. Add a GitHub remote.
7. Commit with a production-scope message.
8. Push `main`.

## Suggested First Commit

```text
feat: prepare wh-cli CLI/API production surface
```

## Release Tags

Use semantic versioning:

```powershell
git tag v1.0.0-rc1
git push origin main --tags
```

The release workflow runs GoReleaser on `v*` tags.

## User-Facing Command Surface

Primary:

- `wh-cli status`
- `wh-cli setup`
- `wh-cli qr`
- `wh-cli chats`
- `wh-cli resolve`
- `wh-cli messages`
- `wh-cli send`
- `wh-cli watch`

Advanced:

- `wh-cli daemon`
- `wh-cli install`
- `wh-cli contacts`
- `wh-cli groups`
- `wh-cli devices`
- `wh-cli export`
- `wh-cli import`
- `wh-cli rotate-jwt-secret`
- `wh-cli wipe`
