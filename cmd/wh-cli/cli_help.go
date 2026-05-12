package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
)

type helpCommand struct {
	Name        string
	Usage       string
	Description string
}

func runHelp(_ context.Context, _ []string) error {
	commands := []helpCommand{
		{"help", "wh-cli help", "Muestra esta ayuda."},
		{"install", "wh-cli install [--dir <path>] [--no-path]", "Instala el binario y, en Windows, lo anade al PATH de usuario."},
		{"setup", "wh-cli setup", "Arranca/comprueba el daemon, autentica la CLI local y muestra el QR si hace falta."},
		{"daemon", "wh-cli daemon [--listen 127.0.0.1:7777]", "Arranca el daemon local de WhatsApp/API."},
		{"status", "wh-cli status", "Muestra el estado de la sesion WhatsApp."},
		{"login", "wh-cli login --passphrase-hash <hash>", "Autentica la CLI contra el daemon y guarda tokens en keyring."},
		{"logout", "wh-cli logout", "Cierra la sesion de la CLI y elimina tokens locales."},
		{"unlock", "wh-cli unlock [--ttl 30m]", "Marca un desbloqueo temporal local."},
		{"qr", "wh-cli qr [--raw|--png <file>|--url]", "Obtiene o renderiza el QR de vinculacion."},
		{"pair-code", "wh-cli pair-code --phone <number>", "Genera codigo de vinculacion por telefono."},
		{"chats", "wh-cli chats [--limit N] [--all] [--json]", "Lista chats recientes en tabla; oculta newsletters/status por defecto."},
		{"resolve", "wh-cli resolve <name-or-jid> [--type chat|dm|group] [--json]", "Busca el JID de un contacto, chat o grupo por nombre."},
		{"messages", "wh-cli messages <name-or-jid> [--limit N] [--json]", "Muestra mensajes de un chat."},
		{"contacts", "wh-cli contacts [--limit N] [--all] [--json]", "Lista contactos conocidos en tabla."},
		{"contact alias", "wh-cli contact alias <jid> <alias>", "Define un alias local para un contacto."},
		{"groups", "wh-cli groups [--json]", "Lista grupos conocidos."},
		{"group participants", "wh-cli group participants <group-name-or-jid> [--json]", "Lista participantes de un grupo."},
		{"send", "wh-cli send <name-or-jid> <text>", "Envia un mensaje de texto."},
		{"send media", "wh-cli send <name-or-jid> --file <path> [--caption <text>]", "Envia un archivo o imagen."},
		{"send audio", "wh-cli send <name-or-jid> --audio <path>", "Envia audio."},
		{"react", "wh-cli react <name-or-jid> <message_id> <emoji>", "Reacciona a un mensaje."},
		{"reply", "wh-cli reply <name-or-jid> <message_id> <text>", "Responde citando un mensaje."},
		{"forward", "wh-cli forward <message_id> <target-name-or-jid> [...]", "Reenvia un mensaje."},
		{"watch", "wh-cli watch", "Escucha eventos del daemon en streaming JSONL."},
		{"devices", "wh-cli devices", "Lista dispositivos vinculados."},
		{"devices revoke", "wh-cli devices revoke <jid>", "Revoca un dispositivo vinculado."},
		{"export", "wh-cli export --out <file>", "Exporta una copia cifrada de la base local."},
		{"import", "wh-cli import --in <file>", "Valida una exportacion cifrada para importacion manual."},
		{"rotate-jwt-secret", "wh-cli rotate-jwt-secret", "Rota el secreto JWT y fuerza relogin."},
		{"wipe", "wh-cli wipe", "Borra permanentemente datos locales y sesion."},
	}

	fmt.Fprintln(os.Stdout, "wh-cli - WhatsApp local CLI")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  wh-cli <command> [options]")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Commands:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, cmd := range commands {
		fmt.Fprintf(w, "  %s\t%s\n", cmd.Usage, cmd.Description)
	}
	_ = w.Flush()
	return nil
}
