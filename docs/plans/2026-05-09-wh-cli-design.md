# Diseño wh-cli — 2026-05-09

Documento consolidado del brainstorming inicial. Decisiones tomadas, alternativas descartadas y razonamiento. Sirve como referencia histórica; los documentos vivos son `plan.md`, `AGENTS.md`, `CLAUDE.md`.

## Decisiones tomadas

| # | Decisión | Alternativas descartadas | Razón |
|---|----------|--------------------------|-------|
| 1 | Motor: **whatsmeow (Go)** | Baileys (Node), neonize (Python wrapper) | Más maduro y estable; lo usa nchat con éxito; multi-dispositivo nativo |
| 2 | Forma: **Daemon + TUI + API** | TUI monolítica con API embebida, solo daemon CLI | Permite que el agente trabaje aunque la TUI esté cerrada |
| 3 | Reparto: **TUI a Claude, backend/infra/BD a Codex** | Claude lleva además cliente API o SDK | Frontera limpia vía OpenAPI; cada uno trabaja en paralelo |
| 4 | **Sin SDK** para agentes; solo API REST + OpenAPI | SDKs en Python+TS oficiales | Multiplica mantenimiento, fragmenta comunidad, riesgo seguridad concentrado, YAGNI; agentes LLM consumen REST sin problema |
| 5 | Persistencia: **SQLite único cifrado (SQLCipher)** | DB plana + sesión separada, SQLite + Redis | Simple, portable, postura de seguridad sólida |
| 6 | Auth: **JWT corto + refresh, bind 127.0.0.1** | API key estática, Unix socket sin auth | Rotable, auditable, cross-platform |
| 7 | Resolución nombres: **alias > push_name > agenda > jid** con prioridad fuerte a **PushName siempre visible** | Solo PushName, alias manual sin agenda | Resuelve el dolor más visible de las herramientas existentes |
| 8 | Scope v1: texto + media + audio + reacciones/replies/forwards. Out: stickers, polls, status, llamadas | Incluir todo desde v1 | Foco en lo que cubre el 95% del uso; scope manejable |
| 9 | Distribución: **binarios + Homebrew + Scoop + Docker + git clone** | Solo `go install`, paquetes nativos .deb/.rpm/.msi | Cobertura amplia con poco mantenimiento |
| 10 | Orden de fases: **backend completo → frontend completo → docs/release** | Fases paralelas alternadas | Codex no se bloquea esperando feedback; cuando Claude arranca, la API ya es estable |
| 11 | **Subcomandos `wh` no interactivos** además de TUI y agentes externos | Solo TUI + API | Tres clientes de la misma API: scripting bash sin abrir TUI |
| 12 | **Cifrado serio:** SQLCipher con clave en keyring del SO (Keychain/Secret Service/Credential Manager); fallback passphrase + Argon2id; desbloqueo temporal con TTL; export cifrado; rotación passphrase/JWT; wipe; alertas de dispositivos vinculados | Solo passphrase derivada, sin keyring | Keyring del SO eleva la postura de seguridad sin romper UX; rotación y wipe son requisitos para opensource serio |

## Diferenciadores frente a inspiración

Inspiración visual: `normen/whatscli`, `d99kris/nchat`. **No somos fork.** Construimos identidad propia con:
1. API REST + WS para agentes (whatscli y nchat son TUI puros)
2. PushNames bien resueltos (problema endémico)
3. Seguridad por diseño documentada

## Arquitectura, stack y modelo de datos

Ver `plan.md` secciones 2-4.

## Seguridad

Ver `plan.md` sección 6 y `docs/security.md` (lo escribe Codex en A0/A6).

Resumen:
- Localhost-only por defecto
- JWT HS256 + Argon2id passphrase, refresh con rotación, `jti` revocable
- SQLCipher at-rest
- Pre-commit: gitleaks + detect-secrets + golangci-lint + hook custom (rechaza `*.db`, JIDs reales)
- `.gitignore` exhaustivo
- Releases firmadas con cosign + SLSA
- `SECURITY.md` con disclosure policy + PGP

## Fases

Tres bloques secuenciales:
- **A — Backend (Codex)**: A0..A6, ver `AGENTS.md`
- **B — Frontend (Claude Code)**: B1..B6, ver `CLAUDE.md`, empieza tras tag `backend-v1.0.0-rc1`
- **C — Docs + release**: README visual con capturas (cuenta de prueba), guías, GoReleaser, firma cosign, v1.0.0

## Próximos pasos

1. Codex inicia A0: repo init, CI, `.gitignore`, pre-commit, `openapi.yaml` v0
2. Codex avanza A1..A6 sin bloqueo
3. Tag `backend-v1.0.0-rc1` → Claude arranca B1
4. Claude completa B1..B6 → tag `frontend-v1.0.0-rc1`
5. Bloque C coordinado → release v1.0.0
