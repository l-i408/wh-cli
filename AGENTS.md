# AGENTS.md — Instrucciones para Codex

> Este archivo es para **Codex CLI**. Si eres Claude Code, lee `CLAUDE.md`.

## Tu rol

Te encargas de **todo el backend, infra, base de datos, CLI y documentación pública** del proyecto `wh-cli`.

Decisión de producto vigente: la TUI se descarta para producción v1. El producto público es CLI + daemon + REST/WS API.

Lee primero `plan.md` para tener la visión completa.

## Tu alcance (lo que SÍ tocas)

```
cmd/wh-cli/
  daemon.go              # entrypoint del daemon
  cli.go                 # raíz CLI (cobra/urfave-cli)
  cli_auth.go            # login, logout, unlock, status
  cli_chats.go           # chats, messages
  cli_send.go            # send, react, reply, forward
  cli_contacts.go        # contacts, contact alias
  cli_groups.go          # groups, group participants
  cli_watch.go           # watch (tail eventos WS a stdout JSONL)
  cli_devices.go         # devices, devices revoke
  cli_admin.go           # export, import, rotate-passphrase, rotate-jwt-secret, wipe

internal/wa/             # wrapper sobre whatsmeow
  client.go              # creación cliente, login QR, eventos
  events.go              # mapeo eventos whatsmeow → eventos internos
  send.go                # envío mensajes, media, reacciones
  resolver.go            # PushName + agenda + alias

internal/api/            # handlers HTTP
  router.go
  auth_handler.go
  session_handler.go
  chats_handler.go
  messages_handler.go
  media_handler.go
  contacts_handler.go
  groups_handler.go
  middleware/
    jwt.go
    ratelimit.go
    audit.go
    cors.go

internal/ws/             # WebSocket hub
  hub.go
  client.go
  events.go              # tipos de evento + serialización

internal/auth/
  passphrase.go          # Argon2id derivación
  jwt.go                 # firma/verificación
  refresh.go             # rotación
  store.go               # auth_tokens repo

internal/keyring/        # integración con keyring del SO
  keyring.go             # interfaz común
  darwin.go              # Keychain (build tag darwin)
  linux.go               # Secret Service (build tag linux)
  windows.go             # Credential Manager (build tag windows)
  unlock.go              # gestión TTL de desbloqueo temporal

internal/crypto/
  argon2.go              # KDF
  sqlcipher.go           # apertura DB con clave, rekey
  export.go              # export/import cifrado
  wipe.go                # zero-fill seguro

internal/store/          # SQLite + SQLCipher
  db.go                  # apertura, migraciones, pragma cipher
  contacts_repo.go
  chats_repo.go
  messages_repo.go
  groups_repo.go
  media_repo.go
  audit_repo.go

internal/media/
  download.go            # descarga + dedupe sha256
  upload.go

internal/types/          # generado por oapi-codegen (NO editar a mano)

api/
  openapi.yaml           # contrato — fuente de verdad
  asyncapi.yaml          # eventos WS

migrations/              # SQL versionado (golang-migrate)
  001_initial.up.sql
  001_initial.down.sql
  ...

.github/workflows/
  ci.yml
  release.yml

.pre-commit-config.yaml
.gitignore
.golangci.yml
Dockerfile
Makefile
goreleaser.yaml
go.mod
go.sum

docs/
  architecture.md
  api-contract.md
  security.md
  threat-model.md
  db-schema.md

SECURITY.md
```

## Lo que NO tocas

- `internal/types/` (generado, no editar a mano)

Si necesitas un cambio en el contrato OpenAPI, actualiza `api/openapi.yaml` y regenera tipos con `make openapi`. No edites tipos generados a mano.

## Convenciones Go

- Formato: `gofumpt` (más estricto que `gofmt`)
- Lint: `golangci-lint run` debe pasar sin warnings (config en `.golangci.yml`, incluye `gosec`, `errcheck`, `revive`, `staticcheck`)
- Errores: `errors.New` / `fmt.Errorf` con `%w`. Define errores sentinel en cada paquete cuando se necesiten.
- Logging: `log/slog` con `slog.Default()`. **Nunca** loggear: cuerpos de mensajes, tokens, passphrase, JIDs completos. Usa el helper `internal/logutil.RedactJID(jid)`.
- Concurrencia: contexts en cualquier función que haga I/O. Cancelación propagada desde la señal del daemon.
- Tests: tabla-driven cuando aplique. Mocks de whatsmeow vía interfaces en `internal/wa`. Tests de integración con SQLite real en archivo temporal.

## Comandos clave

```bash
make build          # construye binario en bin/wh-cli
make run            # corre daemon en modo dev (puerto 7777)
make test           # tests unitarios
make test-e2e       # tests E2E (requiere FIXTURE_QR=1 con mock)
make lint           # golangci-lint + gofumpt check
make ci             # lo que corre en CI (lint + test + build)
make migrate-new name=<x>   # crea par de migraciones
make openapi        # regenera tipos Go desde openapi.yaml
make pre-commit     # corre pre-commit en todos los archivos
```

## Reglas de seguridad **obligatorias**

1. **Nunca** commitees: archivos `*.db`, contenido de `session/`, secretos en código, JIDs reales (regex `\d{8,}@s\.whatsapp\.net`). El pre-commit lo bloquea — si falla, **corrige el origen**, no uses `--no-verify`.
2. **Bind por defecto:** `127.0.0.1`. Para abrir a red externa requiere flag `--listen` explícito + cert TLS.
3. **JWT:** access 15 min, refresh 7 días con rotación, `jti` en `auth_tokens`. Secreto JWT guardado en keyring (account `jwt-secret`); rotable vía `wh rotate-jwt-secret`.
4. **SQLCipher:** clave maestra AES-256. Almacenamiento por orden:
   - Default: keyring del SO (account `master-key`) vía `internal/keyring`.
   - Alternativa: derivada de passphrase con Argon2id (`time=3, memory=64MB, threads=4`); salt en `~/.config/wh-cli/salt`.
   La clave **jamás** se persiste en disco en claro.
5. **Desbloqueo temporal:** clave en memoria del daemon con TTL configurable (`wh unlock --ttl 30m`). Tras expirar, cerrar handles DB y devolver `423 Locked` en endpoints de datos.
6. **Keyring del SO** (`internal/keyring`):
   - macOS Keychain, Linux Secret Service, Windows Credential Manager.
   - Vía `github.com/zalando/go-keyring`. Si falla (sin daemon de keyring en Linux headless), fallback a passphrase prompt + warning.
7. **Export/import seguro:** `wh export` re-cifra con passphrase distinta; `wh wipe` zero-fill DB + media + entradas keyring; doble confirmación.
8. **Dispositivos vinculados:** suscribirse a `events.PairSuccess` y polling `GetLinkedDevices()`. Emitir `session.device_linked` por WS y registrar en `audit_log` con severidad alta.
9. **CORS:** allowlist explícita, nunca `*`.
10. **Rate limit:** `/auth/login` 5/min/IP; `/auth/refresh` 30/min; `/auth/unlock` 10/min.
11. **Audit log:** toda acción de escritura con `actor` (`tui`, `cli`, `agent:<label>`).
12. **Logging:** redacta JIDs (helper `logutil.RedactJID`). Cuerpos de mensaje, tokens y passphrase **nunca** se loggean.

## Modelo de datos

Ver `docs/db-schema.md` (lo escribes tú en A0). Tablas mínimas:

```
device_session, contacts, groups, group_participants, chats, messages,
media_blobs, auth_tokens, audit_log
```

## Flujo de fases (Bloque A)

1. **A0** — Repo init, CI, pre-commit, `openapi.yaml` v0, `Makefile`, `.gitignore`, esqueleto Go (`go mod init github.com/<org>/wh-cli`). Criterio: `make ci` verde.
2. **A1** — `internal/keyring` + `internal/crypto` + `internal/store` con SQLCipher, `internal/auth` con JWT, `internal/wa` mínimo (login QR, sesión persistida cifrada), endpoints `/auth/*` y `/session/*`, subcomandos `wh login/logout/unlock/status/qr`. Criterio: escanear QR → reiniciar daemon → `wh unlock` → sesión sigue conectada.
3. **A2** — Chats, mensajes texto, hub WebSocket, eventos `message.new`/`chat.updated`. Subcomandos `wh chats`, `wh messages`, `wh send`, `wh watch`. Criterio: round-trip texto con PushName correcto.
4. **A3** — Media + audio. Dedupe sha256. Subcomandos `wh send --file/--audio`. Criterio: enviar imagen, recibirla en cuenta de prueba, descargar y comparar hash.
5. **A4** — Reacciones, replies, forwards. Subcomandos `wh react/reply/forward`. Criterio: agente curl conduce conversación natural.
6. **A5** — Grupos + resolver de nombres. Subcomandos `wh groups`, `wh group participants`, `wh contact alias`. Criterio: en grupo de prueba, todos los participantes muestran PushName, ningún JID crudo.
7. **A6** — Gestión avanzada de credenciales: `wh export/import/rotate-passphrase/rotate-jwt-secret/wipe/devices`. Evento `session.device_linked`. E2E tests + hardening + `audit_log` completo. Criterio: coverage ≥70% en `internal/{api,auth,store,crypto,keyring}`, `gosec` sin issues HIGH, rotación de passphrase y JWT funcionan sin pérdida de datos.

## Cuando termines el Bloque A

Notifica al usuario y haz un tag `backend-v1.0.0-rc1`. Claude Code arrancará el Bloque B sobre ese tag.

## Documentación que escribes tú

Cada decisión técnica que tomes se documenta. Mantén actualizados:
- `docs/architecture.md` — diagramas de secuencia (login QR, envío mensaje, recepción)
- `docs/api-contract.md` — explicación de cada endpoint y por qué
- `docs/db-schema.md` — tablas, índices, justificación
- `docs/security.md` — modelo de amenazas, decisiones de cifrado/auth
- `docs/threat-model.md` — STRIDE simplificado
- `SECURITY.md` — disclosure policy con email + PGP key

El usuario quiere que esto sea suficiente para que cualquier contributor entienda el sistema sin preguntarte.
