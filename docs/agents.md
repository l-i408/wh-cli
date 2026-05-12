# Agent integration

Agents should use `wh-cli` commands first and the HTTP API when they need lower-level control.

## Recommended CLI Flow

Resolve the target:

```powershell
wh-cli resolve "Marta" --json
```

Read context:

```powershell
wh-cli messages "Marta" --limit 50 --json
```

Send:

```powershell
wh-cli send "Marta" "Hola, soy el agente."
```

Watch for new events:

```powershell
wh-cli watch
```

## Ambiguous Names

When a name is ambiguous, `wh-cli` exits with code `4` and prints candidates:

```text
invalid input: ambiguous target "Marta"
1. Marta [dm] ...
2. Marta Catequista [dm] ...
```

The agent should ask the user to choose a more specific name or use the exact JID from `resolve --json`.

## API

The daemon listens on `http://127.0.0.1:7777` by default.

Useful endpoints:

- `POST /auth/login`
- `POST /auth/refresh`
- `GET /session/status`
- `GET /chats`
- `GET /chats/{jid}/messages`
- `POST /messages`
- `GET /groups`
- `GET /groups/{jid}/participants`
- `GET /contacts`
- `GET /ws`

The OpenAPI contract is [api/openapi.yaml](../api/openapi.yaml).

## Safety

Agents must not log message bodies, tokens, passphrases or full JIDs unless the user explicitly asks for diagnostic output. Prefer `resolve` names in user-facing logs and keep raw JIDs internal.
