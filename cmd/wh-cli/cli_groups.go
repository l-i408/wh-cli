package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

type cliGroupPage struct {
	Items []cliGroup `json:"items"`
}

type cliGroup struct {
	JID              string `json:"jid"`
	Name             string `json:"name"`
	Topic            string `json:"topic"`
	OwnerJID         string `json:"owner_jid"`
	OwnerDisplayName string `json:"owner_display_name"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type cliGroupParticipantPage struct {
	Items []cliGroupParticipant `json:"items"`
}

type cliGroupParticipant struct {
	GroupJID    string `json:"group_jid"`
	ContactJID  string `json:"contact_jid"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

func runGroups(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("groups", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%w: usage groups", errInvalidInput)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	body, err := httpGetAuth(ctx, *addr+"/groups", token)
	if err != nil {
		return err
	}
	if *jsonOut {
		printJSONBody(body)
		return nil
	}
	var page cliGroupPage
	if err := json.Unmarshal(body, &page); err != nil {
		return fmt.Errorf("decode groups response: %w", err)
	}
	printGroupTable(page.Items)
	return nil
}

func runGroup(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: usage group participants <group_jid>", errInvalidInput)
	}
	switch args[0] {
	case "participants":
		return runGroupParticipants(ctx, args[1:])
	default:
		return fmt.Errorf("%w: unknown group command %s", errInvalidInput, args[0])
	}
}

func runGroupParticipants(ctx context.Context, args []string) error {
	opts, err := parseGroupParticipantsArgs(args)
	if err != nil {
		return err
	}
	if opts.groupJID == "" {
		return fmt.Errorf("%w: usage group participants <group_jid>", errInvalidInput)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	target, err := resolveSingleTarget(ctx, opts.addr, token, opts.groupJID, "group")
	if err != nil {
		return err
	}
	body, err := httpGetAuth(ctx, fmt.Sprintf("%s/groups/%s/participants", opts.addr, target.JID), token)
	if err != nil {
		return err
	}
	if opts.json {
		printJSONBody(body)
		return nil
	}
	var page cliGroupParticipantPage
	if err := json.Unmarshal(body, &page); err != nil {
		return fmt.Errorf("decode participants response: %w", err)
	}
	printGroupParticipantsTable(page.Items)
	return nil
}

func printGroupTable(groups []cliGroup) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "UPDATED\tNAME\tOWNER\tJID")
	for _, group := range groups {
		name := group.Name
		if name == "" {
			name = "(sin nombre)"
		}
		owner := group.OwnerDisplayName
		if owner == "" {
			owner = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			formatGroupTime(group.UpdatedAt),
			truncateText(name, 42),
			truncateText(owner, 24),
			group.JID,
		)
	}
	_ = w.Flush()
}

func printGroupParticipantsTable(participants []cliGroupParticipant) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ROLE\tNAME\tJID")
	for _, participant := range participants {
		name := participant.DisplayName
		if name == "" {
			name = "(sin nombre)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", participant.Role, truncateText(name, 36), participant.ContactJID)
	}
	_ = w.Flush()
}

type groupParticipantsOptions struct {
	addr     string
	json     bool
	groupJID string
}

func parseGroupParticipantsArgs(args []string) (groupParticipantsOptions, error) {
	opts := groupParticipantsOptions{addr: defaultDaemonAddr}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--addr":
			value, ok := nextArg(args, &i)
			if !ok {
				return groupParticipantsOptions{}, fmt.Errorf("%w: --addr requires a value", errInvalidInput)
			}
			opts.addr = value
		case "--json":
			opts.json = true
		default:
			if opts.groupJID != "" {
				return groupParticipantsOptions{}, fmt.Errorf("%w: usage group participants <group_jid>", errInvalidInput)
			}
			opts.groupJID = arg
		}
	}
	return opts, nil
}

func formatGroupTime(value string) string {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "-"
	}
	return t.Local().Format("2006-01-02")
}
