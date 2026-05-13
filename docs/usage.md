# wh-cli usage

`wh-cli` is a command-first WhatsApp client for local automation.

## First Run

```powershell
wh-cli setup
wh-cli status
```

`setup` starts the daemon if needed, prepares local CLI authentication, shows the QR when the WhatsApp session is not linked, and waits for a connected session.

## Human Output And JSON

Most read commands print tables by default:

```powershell
wh-cli chats
wh-cli groups
wh-cli contacts --limit 20
```

Use `--json` for scripts:

```powershell
wh-cli chats --json
wh-cli messages "Marta" --json
```

## Name Resolution

Commands accept a display name or a raw JID:

```powershell
wh-cli resolve "Family"
wh-cli messages "Family"
wh-cli send "Marta" "Hola"
```

If more than one target matches, the command exits with invalid input and prints candidates. Use a more specific name or the exact JID.

## Messages

```powershell
wh-cli messages "Marta" --limit 50
wh-cli send "Marta" "Hola"
wh-cli send "Marta" --file .\image.png --caption "Foto"
wh-cli send "Marta" --audio .\voice.ogg
```

## Groups And Contacts

```powershell
wh-cli groups
wh-cli group participants "Family"
wh-cli contacts --limit 50
wh-cli contact alias <jid> "Nombre corto"
```

## Events

```powershell
wh-cli watch
```

This keeps running and prints daemon events as JSON lines.

## Admin

Use these carefully:

```powershell
wh-cli devices
wh-cli devices revoke <jid>
wh-cli export --out backup.whcli.enc
wh-cli import --in backup.whcli.enc
wh-cli rotate-jwt-secret
wh-cli wipe
```

`wipe` permanently removes local data and requires confirmation.
