# Threat Model

This is a simplified STRIDE model for the local daemon.

## Spoofing

Risk: another local process pretends to be the CLI or an approved local agent.

Mitigation: JWT authentication, short access tokens, refresh rotation, token `jti` registry, and actor labels for audit events.

## Tampering

Risk: local files are modified or replaced.

Mitigation: SQLCipher encryption, migrations with explicit schema, signed releases, and future export integrity checks.

## Repudiation

Risk: a client denies sending or mutating data.

Mitigation: append write actions to `audit_log` with actor, action, target, result, timestamp, and severity.

## Information Disclosure

Risk: message contents, sessions, tokens, or JIDs leak through logs or repository files.

Mitigation: encrypted storage, keyring-backed secrets, redacted JID logging, pre-commit checks, and no message body logging.

## Denial of Service

Risk: login or unlock endpoints are brute-forced or spammed.

Mitigation: auth middleware rate-limits `/auth/login`, `/auth/refresh`, and `/auth/unlock`.

## Elevation of Privilege

Risk: remote network access exposes the local automation API.

Mitigation: bind to loopback by default and reject non-local listen addresses until explicit TLS configuration is implemented.
