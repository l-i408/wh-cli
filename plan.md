# wh-cli — Plan de proyecto

> WhatsApp en tu terminal. Construido para humanos y para agentes de IA.

## 1. Visión

`wh-cli` es una herramienta opensource de línea de comandos que conecta tu cuenta personal de WhatsApp (vía QR multi-dispositivo) y expone una API HTTP/WS local para que cualquier agente de IA — Claude Code, Codex CLI, OpenClaw, Kimi CLI, etc. — pueda automatizar mensajería: leer chats, enviar mensajes, reaccionar, gestionar grupos, descargar media. La interfaz principal es CLI scriptable; la TUI queda fuera del scope de producción.

Inspirada visualmente en [`normen/whatscli`](https://github.com/normen/whatscli) y [`d99kris/nchat`](https://github.com/d99kris/nchat), pero con tres diferenciadores:

1. **Agent-first:** API REST + WebSocket documentada con OpenAPI 3.1 desde el día uno. whatscli y nchat son TUI puros y no dejan que un agente las controle de forma fiable.
2. **PushNames bien resueltos:** problema endémico de las herramientas existentes — los grupos muestran JIDs/números en vez de nombres. En `wh-cli` el `PushName` es prioritario y siempre se ve.
3. **Seguridad por diseño:** localhost-only, JWT con refresh y rotación, SQLite cifrado con SQLCipher, política dura de secretos para repo opensource.

**No somos un fork** de whatscli ni nchat. Reusamos `whatsmeow` (la librería Go que usan otros) y nos inspiramos en su estética TUI, pero la arquitectura, la API y la base de código son propias.

## 2. Stack

### Backend / Daemon (responsabilidad de **Codex**)

| Pieza | Elección | Razón |
|-------|----------|-------|
| Lenguaje | Go 1.22+ | Mismo lenguaje que la TUI; un solo binario; whatsmeow es Go nativo |
| WhatsApp | `go.mau.fi/whatsmeow` | Librería más madura, multi-dispositivo, mantenida |
| Router HTTP | `chi` | Minimalista, idiomático Go, middleware estándar `net/http` |
| WebSocket | `nhooyr.io/websocket` | API moderna, context-aware |
| DB | SQLite + SQLCipher (`mattn/go-sqlite3` con tags) | Embedded, cifrado at-rest, portable |
| Migraciones | `golang-migrate` | Estándar de facto |
| JWT | `golang-jwt/jwt/v5` HS256 | Clave derivada de passphrase con Argon2id |
| Config | `viper` + flags | Soporta env vars y archivos |
| Logging | `slog` (stdlib) | Structured logging con redacción de secretos |
| QR render | `mdp/qrterminal` | ASCII estable para terminal |

### Frontend / TUI

La TUI fue descartada para v1. El producto se centra en CLI humana, CLI para agentes y API HTTP/WS local.

### Contrato

- **`api/openapi.yaml`** — única fuente de verdad
- Tipos Go generados con `oapi-codegen` (`internal/types/`)
- AsyncAPI ligero para eventos WebSocket en `api/asyncapi.yaml`

### Infra (responsabilidad de **Codex**)

- CI: GitHub Actions (lint + test + build matrix Linux/macOS/Windows)
- Release: GoReleaser → binarios firmados con cosign + Homebrew tap + Scoop bucket + Docker Hub multi-arch
- Pre-commit: `gitleaks`, `detect-secrets`, `golangci-lint` (incluye `gosec`), `gofumpt`

## 3. Arquitectura

```
┌──────────────┐       ┌──────────────┐
│   TUI        │       │  Agente IA   │
│ (Bubble Tea) │       │ (Claude/Codex│
│              │       │  /Kimi/...)  │
└──────┬───────┘       └──────┬───────┘
       │ HTTP + WS            │ HTTP + WS
       │ (JWT, 127.0.0.1)     │ (JWT, 127.0.0.1)
       └──────────┬───────────┘
                  ▼
        ┌──────────────────────┐
        │   Daemon  (Go)       │
        │  ┌────────────────┐  │
        │  │ API REST + WS  │  │
        │  │ Auth (JWT)     │  │
        │  │ Event bus      │  │
        │  └────────┬───────┘  │
        │  ┌────────▼───────┐  │
        │  │  whatsmeow     │  │
        │  │  WA session    │  │
        │  └────────┬───────┘  │
        │  ┌────────▼───────┐  │
        │  │ SQLite cifrado │  │
        │  │ (SQLCipher)    │  │
        │  └────────────────┘  │
        └──────────────────────┘
```

Dos modos de cliente sobre la misma API, un binario `wh-cli`:
- `wh daemon` — proceso de fondo, mantiene sesión WA y sirve API en `127.0.0.1:7777`
- `wh-cli <subcomando>` — CLI no interactivo para humanos, scripting y agentes

### Subcomandos `wh` (CLI no interactivo)

Equivalente funcional de la TUI sin interactividad, pensado para scripting:

```
wh login | logout | status | qr | unlock --ttl 30m
wh chats [--json] [--limit N]
wh messages <chat> [--before ID] [--limit N] [--json]
wh send <chat> "texto" | --file path | --audio path
wh react <msg_id> <emoji>
wh reply <msg_id> "texto"
wh forward <msg_id> <chat1> <chat2> ...
wh contacts | contact alias <jid> "Alias"
wh groups | group participants <jid>
wh watch [--chat <jid>] [--types ...]   # tail JSONL de eventos WS
wh devices | devices revoke <id>
wh export --out file.enc | rotate-passphrase | rotate-jwt-secret | wipe
```

Reglas:
- `--json` produce JSON estricto pipe-friendly; sin flag, salida humana.
- Códigos de salida: 0 ok, 1 error, 2 auth, 3 daemon no corriendo, 4 input inválido, 5 locked.
- Token leído del keyring del SO (ver §6) — nunca flags ni env por defecto.

## 4. Modelo de datos

SQLite cifrado con SQLCipher. Clave derivada de passphrase del usuario vía Argon2id (salt en archivo de config, no cifrado).

```
device_session       -- sesión whatsmeow
contacts             -- jid, push_name, agenda_name, alias, avatar_path, updated_at
groups               -- jid, name, topic, owner_jid, created_at
group_participants   -- group_jid, contact_jid, role, joined_at
chats                -- jid, type (dm/group), last_message_id, unread_count, pinned, muted_until
messages             -- id, chat_jid, sender_jid, type, body, media_path,
                        reply_to_id, reactions_json, status, timestamp
media_blobs          -- id, message_id, mime, size, sha256, local_path, downloaded
auth_tokens          -- jti, kind, client_label, issued_at, expires_at, revoked
audit_log            -- ts, actor, action, target, result
```

**Resolución de nombres** (estrategia):
```
display_name(jid) = alias ?? push_name ?? agenda_name ?? format_phone(jid)
```
`push_name` debe ser prioritario y refrescarse en cada `messageInfo` recibido.

## 5. API (resumen, detalle en `api/openapi.yaml`)

### REST
- `POST /auth/login` — passphrase → access + refresh
- `POST /auth/refresh` — refresh → nuevo par
- `POST /auth/logout` — revoca tokens
- `GET /session/qr` — stream del QR (SSE) cuando no hay sesión
- `GET /session/status` — connected | qr_pending | logged_out
- `POST /session/logout`
- `GET /chats?limit=&cursor=`
- `GET /chats/{jid}/messages?before=&limit=`
- `POST /messages` — body: `{chat_jid, type, body, media_id?}`
- `POST /messages/{id}/react` — `{emoji}`
- `POST /messages/{id}/reply` — `{body, type}`
- `POST /messages/{id}/forward` — `{to_jids: []}`
- `GET /contacts`, `PATCH /contacts/{jid}` (alias)
- `GET /groups/{jid}`, `GET /groups/{jid}/participants`
- `POST /media/upload` (multipart) → `{media_id}`
- `GET /media/{id}` — stream

### WebSocket `/ws`
Eventos JSON tipados:
- `message.new`, `message.update`, `message.reaction`, `message.deleted`
- `chat.updated`, `presence.update`, `typing.update`
- `group.participants_changed`, `group.metadata_changed`
- `session.qr`, `session.connected`, `session.disconnected`, `session.logged_out`

Suscripción con filtros opcionales (`types`, `chat_jid`).

## 6. Seguridad y gestión de credenciales (detalle en `docs/security.md`)

### Cifrado at-rest (DB y sesión)

- **SQLCipher** con clave maestra AES-256. La clave **no se persiste en disco en claro**.
- Dos modos de unlock, configurables:
  1. **Keyring del SO** (recomendado, default): clave maestra guardada en
     - macOS → **Keychain**
     - Linux → **Secret Service** (libsecret / GNOME Keyring / KWallet)
     - Windows → **Credential Manager**
     Vía `github.com/zalando/go-keyring`. Servicio `wh-cli`, account `master-key`.
  2. **Passphrase prompt**: cada arranque pregunta. Clave derivada con Argon2id (`time=3, memory=64MB, threads=4`, salt en `~/.config/wh-cli/salt`).
- **Desbloqueo temporal:** `wh unlock --ttl 30m`. Mantiene la clave en memoria del daemon hasta que expira el TTL; después, la DB se cierra y endpoints sensibles devuelven `423 Locked`.
- Media en `~/.local/share/wh-cli/media/<sha256>`, permisos `0600`. Nombre por sha256 evita filtración del remitente en metadatos del filesystem.

### Autenticación de la API

- **JWT HS256** firmado con secreto rotable guardado también en keyring (account `jwt-secret`). Access 15 min, refresh 7 días con rotación.
- `jti` registrado en `auth_tokens` para revocación real.
- **Cookies** `HttpOnly; Secure; SameSite=Strict` para la TUI (cuando se usa proxy local TLS); Bearer para CLI/agentes.
- **Bind:** `127.0.0.1` por defecto. Modo remoto opt-in con TLS obligatorio + flag explícito + warning de startup.
- **Rate limiting:** `/auth/login` 5/min/IP; `/auth/refresh` 30/min.

### Gestión del ciclo de vida de credenciales

- `wh rotate-passphrase` — re-deriva clave, re-cifra DB en lugar (transaccional con archivo `.tmp` + rename).
- `wh rotate-jwt-secret` — invalida todos los tokens activos, fuerza re-login en TUI/CLI/agentes.
- `wh export --out backup.whcli.enc` — re-cifra con passphrase distinta del usuario; nunca exporta en claro.
- `wh import --in backup.whcli.enc` — restaura.
- `wh wipe` — zero-fill DB + media + entradas keyring + sesión whatsmeow. Doble confirmación.

### Dispositivos vinculados

- Daemon escucha el evento whatsmeow `events.PairSuccess` y consulta `GetLinkedDevices()` periódicamente.
- Emite evento WS `session.device_linked` con metadatos (timestamp, plataforma, nombre).
- TUI muestra notificación destacada; CLI lo expone vía `wh watch --types session.device_linked`.
- `wh devices` lista; `wh devices revoke <id>` desvincula vía whatsmeow.
- Se loggea al `audit_log` con severidad alta.

### Logging y observabilidad

- `slog` con redacción automática: cuerpos de mensaje, tokens, passphrase y JIDs completos jamás se loggean (helper `logutil.RedactJID`).
- `audit_log` registra toda acción de escritura con actor (`tui`, `cli`, `agent:<label>`).

### Higiene del repo opensource

- **Pre-commit:** `gitleaks`, `detect-secrets`, `golangci-lint` (incluye `gosec`), `gofumpt`, hook custom que rechaza `*.db`, `session/`, JIDs reales en código (regex `\d{8,}@s\.whatsapp\.net`).
- **`.gitignore`** exhaustivo (ver `docs/security.md`).
- **Releases firmadas** con cosign + checksums SHA256 + atestación SLSA vía GoReleaser.
- **`SECURITY.md`** con disclosure policy, email de seguridad y PGP key.
- Capturas para README usan **cuenta de prueba dedicada** con contactos ficticios.

## 7. Fases de implementación

Orden secuencial para minimizar bloqueos: **todo el backend → todo el frontend → documentación + release**.

### Bloque A — Backend (Codex)

| Fase | Entrega | Criterio de aceptación |
|------|---------|------------------------|
| A0 | Repo init, `.gitignore`, pre-commit, CI, `openapi.yaml` v0 | `make ci` verde, gitleaks limpio |
| A1 | Daemon + JWT + SQLCipher + login QR + sesión persistida | Escanear QR, reiniciar, sesión vive |
| A2 | Chats + mensajes texto + WS eventos | Round-trip texto DM con PushName correcto |
| A3 | Media (img/doc) + audio (opus) | Envío/recepción con dedupe sha256 |
| A4 | Reacciones, replies, forwards | Agente puede conducir conversación natural |
| A5 | Grupos + resolver de nombres | Cero JIDs visibles con grupos de prueba |
| A6 | E2E tests + hardening + audit log | Coverage ≥70% en `internal/{api,auth,store}` |

### Bloque B — Frontend (Claude Code)

| Fase | Entrega | Criterio de aceptación |
|------|---------|------------------------|
| B1 | Cliente HTTP/WS + login TUI + render QR | Login funciona contra daemon |
| B2 | Lista chats + vista chat + envío texto | Navegación fluida con keybindings |
| B3 | Render media + audio | Imágenes en terminales compatibles, fallback texto |
| B4 | Reacciones, replies, forwards en TUI | Acciones disponibles con atajos |
| B5 | Vista grupos + contactos + alias | PushName visible siempre, alias editable |
| B6 | Theming, polish UX, help screen | Light/dark, demo GIF presentable |

### Bloque C — Documentación + release

| Fase | Entrega |
|------|---------|
| C1 | `README.md` visual con capturas/GIF (cuenta de prueba dedicada) |
| C2 | `docs/usage.md`, `docs/agents.md`, `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md` |
| C3 | GoReleaser + Homebrew tap + Scoop bucket + Docker Hub + firma cosign |
| C4 | Release v1.0.0 |

## 8. Scope v1 (lo que entra)

- Texto, imágenes, documentos (envío + recepción)
- Audios y notas de voz (opus)
- Reacciones, replies, forwards
- Grupos completos con resolución de nombres
- Login QR multi-dispositivo, sesión persistida
- API REST + WS para agentes
- TUI completa con theming
- Distribución: binarios + Homebrew + Scoop + Docker + `git clone && make`

## 9. Out of scope v1 (explícito)

- Stickers, polls, status/historias, llamadas → v1.1
- Multi-cuenta → v1.2
- UI gráfica (web/desktop)
- Plugins/extensions
- Sincronización entre instancias de wh-cli

## 10. Reparto de archivos

Ver `AGENTS.md` (Codex) y `CLAUDE.md` (Claude Code) para el detalle de qué archivos toca cada uno y las reglas de no-pisado.
