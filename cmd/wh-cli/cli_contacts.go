package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
)

type cliContactPage struct {
	Items []cliContact `json:"items"`
}

type cliContact struct {
	JID         string `json:"jid"`
	PushName    string `json:"push_name"`
	AgendaName  string `json:"agenda_name"`
	Alias       string `json:"alias"`
	DisplayName string `json:"display_name"`
	UpdatedAt   string `json:"updated_at"`
}

func runContacts(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("contacts", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	limit := fs.Int("limit", 50, "maximum contacts to print")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	all := fs.Bool("all", false, "print all contacts")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%w: usage contacts", errInvalidInput)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	body, err := httpGetAuth(ctx, *addr+"/contacts", token)
	if err != nil {
		return err
	}
	if *jsonOut {
		printJSONBody(body)
		return nil
	}
	var page cliContactPage
	if err := json.Unmarshal(body, &page); err != nil {
		return fmt.Errorf("decode contacts response: %w", err)
	}
	printContactTable(page.Items, *limit, *all)
	return nil
}

func runContact(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: usage contact alias <jid> <alias>", errInvalidInput)
	}
	switch args[0] {
	case "alias":
		return runContactAlias(ctx, args[1:])
	default:
		return fmt.Errorf("%w: unknown contact command %s", errInvalidInput, args[0])
	}
}

func runContactAlias(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("contact alias", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("%w: usage contact alias <jid> <alias>", errInvalidInput)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	body, err := httpPatchJSON(ctx, fmt.Sprintf("%s/contacts/%s", *addr, fs.Arg(0)), token, map[string]string{"alias": fs.Arg(1)})
	if err != nil {
		return err
	}
	printJSONBody(body)
	return nil
}

func printContactTable(contacts []cliContact, limit int, all bool) {
	if limit <= 0 {
		limit = 50
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tAGENDA\tPUSH\tJID")
	printed := 0
	for _, contact := range contacts {
		if !all && printed >= limit {
			break
		}
		name := contact.DisplayName
		if name == "" {
			name = "(sin nombre)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			truncateText(name, 32),
			truncateText(contact.AgendaName, 24),
			truncateText(contact.PushName, 24),
			contact.JID,
		)
		printed++
	}
	_ = w.Flush()
	if !all && len(contacts) > printed {
		fmt.Fprintf(os.Stdout, "\nShowing %d of %d contacts. Use --all or --json for the full list.\n", printed, len(contacts))
	}
}
