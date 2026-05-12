# wh-cli

Your WhatsApp, in your terminal. Built for humans and AI agents.

`wh-cli` links a WhatsApp account through the official multi-device QR flow, runs a localhost-only daemon, and exposes both a human-friendly CLI and a scriptable HTTP/WebSocket API.

> This project is not affiliated with WhatsApp or Meta. Use it responsibly and follow WhatsApp's terms.

## Features

- Local WhatsApp daemon on `127.0.0.1` by default.
- CLI commands for chats, messages, sending, media, groups, contacts, devices, export and wipe.
- Name resolution: commands accept names such as `"Marta"` or `"Catequesis Beniparrell"` as well as raw JIDs.
- JSON output with `--json` for agents and scripts.
- REST API and WebSocket event stream for external automation.
- SQLite storage, keyring-backed secrets and JWT auth.

## Install

From a release archive, put `wh-cli`/`wh-cli.exe` somewhere on your machine and run:

```powershell
wh-cli install
```

On Windows this copies the binary to `%LOCALAPPDATA%\Programs\wh-cli` and adds it to the user `PATH`.

From source:

```powershell
git clone https://github.com/l-i408/wh-cli
cd wh-cli
make build
.\bin\wh-cli.exe install
```

## Quick Start

```powershell
wh-cli setup
wh-cli chats
```

Once connected:

```powershell
wh-cli resolve "Marta"
wh-cli messages "Marta" --limit 20
wh-cli send "Marta" "Hola desde wh-cli"
wh-cli watch
```

## Common Commands

| Command | Purpose |
| --- | --- |
| `wh-cli help` | Show all commands. |
| `wh-cli setup` | Start/check the daemon, authenticate the local CLI and show the QR if needed. |
| `wh-cli daemon` | Start the local WhatsApp/API daemon. |
| `wh-cli status` | Show WhatsApp session status. |
| `wh-cli qr` | Render the pairing QR in the terminal. |
| `wh-cli chats` | List recent chats. |
| `wh-cli resolve <name>` | Resolve a contact/group/chat name to a JID. |
| `wh-cli messages <name-or-jid>` | Show chat messages. |
| `wh-cli send <name-or-jid> <text>` | Send a text message. |
| `wh-cli groups` | List groups. |
| `wh-cli contacts --limit 20` | List known contacts. |
| `wh-cli devices` | List linked devices. |
| `wh-cli watch` | Stream daemon events as JSON. |

Use `--json` on list/read commands when integrating with agents.

## For Agents

Agents should prefer these commands:

```powershell
wh-cli resolve "Target name" --json
wh-cli messages "Target name" --limit 50 --json
wh-cli send "Target name" "message"
wh-cli watch
```

If a target name is ambiguous, `wh-cli` exits with code `4` and prints candidate names/JIDs. The agent should ask the user for a more specific name or use the exact JID returned by `resolve`.

See [docs/agents.md](docs/agents.md) and [api/openapi.yaml](api/openapi.yaml).

## Production Scope

The v1 production surface is CLI + daemon + REST/WS API. The previous Bubble Tea TUI experiment is intentionally not shipped.

Advanced/admin commands such as `install`, `daemon`, `export`, `import`, `rotate-jwt-secret`, `devices revoke` and `wipe` are kept for local operation and recovery, but the normal user flow is `setup`, `status`, `qr`, `chats`, `resolve`, `messages`, `send` and `watch`.

## Security

- The daemon binds to `127.0.0.1` by default.
- Tokens and master secrets are stored through the OS keyring.
- Do not commit databases, sessions, QR images, logs, message bodies or real WhatsApp identifiers.

See [SECURITY.md](SECURITY.md).

## Development

```powershell
make test
make build
make ci
```

## License

MIT. See `LICENSE` once published.
