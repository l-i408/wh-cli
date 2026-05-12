package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/l-i408/wh-cli/internal/keyring"
)

type cliChatPage struct {
	Items []cliChat `json:"items"`
}

type cliChat struct {
	JID         string  `json:"jid"`
	Type        string  `json:"type"`
	DisplayName string  `json:"display_name"`
	LastMsgID   *string `json:"last_message_id"`
	UnreadCount int     `json:"unread_count"`
	UpdatedAt   string  `json:"updated_at"`
}

type cliMessagePage struct {
	Items []cliMessage `json:"items"`
}

type cliMessage struct {
	ID        string  `json:"id"`
	ChatJID   string  `json:"chat_jid"`
	SenderJID string  `json:"sender_jid"`
	Type      string  `json:"type"`
	Body      *string `json:"body"`
	MediaID   *string `json:"media_id"`
	MediaPath *string `json:"media_path"`
	Status    *string `json:"status"`
	Timestamp string  `json:"timestamp"`
}

func runChats(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("chats", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	limit := fs.Int("limit", 50, "maximum chats")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	all := fs.Bool("all", false, "include newsletters and broadcasts")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	body, err := httpGetAuth(ctx, fmt.Sprintf("%s/chats?limit=%d", *addr, *limit), token)
	if err != nil {
		return err
	}
	if *jsonOut {
		printJSONBody(body)
		return nil
	}
	var page cliChatPage
	if err := json.Unmarshal(body, &page); err != nil {
		return fmt.Errorf("decode chats response: %w", err)
	}
	printChatTable(page.Items, *all)
	return nil
}

func runMessages(ctx context.Context, args []string) error {
	opts, err := parseMessagesArgs(args)
	if err != nil {
		return err
	}
	if opts.chatJID == "" {
		return fmt.Errorf("%w: usage messages <chat_jid>", errInvalidInput)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	target, err := resolveSingleTarget(ctx, opts.addr, token, opts.chatJID, "chat")
	if err != nil {
		return err
	}
	body, err := httpGetAuth(ctx, chatPath(opts.addr, target.JID, fmt.Sprintf("/messages?limit=%d", opts.limit)), token)
	if err != nil {
		return err
	}
	if opts.json {
		printJSONBody(body)
		return nil
	}
	var page cliMessagePage
	if err := json.Unmarshal(body, &page); err != nil {
		return fmt.Errorf("decode messages response: %w", err)
	}
	printMessageTable(page.Items, target.JID)
	return nil
}

func cliAccessToken(ctx context.Context) (string, error) {
	kr := keyring.NewOSStore()
	token, err := kr.Get(ctx, keyring.AccountAccessToken)
	if err != nil {
		return "", errAuth
	}
	return string(token), nil
}

type messagesOptions struct {
	addr    string
	limit   int
	json    bool
	chatJID string
}

func parseMessagesArgs(args []string) (messagesOptions, error) {
	opts := messagesOptions{addr: defaultDaemonAddr, limit: 50}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--addr":
			value, ok := nextArg(args, &i)
			if !ok {
				return messagesOptions{}, fmt.Errorf("%w: --addr requires a value", errInvalidInput)
			}
			opts.addr = value
		case "--limit":
			value, ok := nextArg(args, &i)
			if !ok {
				return messagesOptions{}, fmt.Errorf("%w: --limit requires a value", errInvalidInput)
			}
			if _, err := fmt.Sscanf(value, "%d", &opts.limit); err != nil {
				return messagesOptions{}, fmt.Errorf("%w: invalid --limit", errInvalidInput)
			}
		case "--json":
			opts.json = true
		default:
			if strings.HasPrefix(arg, "-") {
				return messagesOptions{}, fmt.Errorf("%w: unknown messages option %s", errInvalidInput, arg)
			}
			if opts.chatJID != "" {
				return messagesOptions{}, fmt.Errorf("%w: usage messages <chat_jid>", errInvalidInput)
			}
			opts.chatJID = arg
		}
	}
	return opts, nil
}

func printChatTable(chats []cliChat, includeAll bool) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "WHEN\tUNREAD\tTYPE\tNAME\tJID")
	for _, chat := range chats {
		if !includeAll && isAuxiliaryChat(chat.JID) {
			continue
		}
		name := chat.DisplayName
		if name == "" {
			name = "(sin nombre)"
		}
		fmt.Fprintf(
			w, "%s\t%d\t%s\t%s\t%s\n",
			formatCLITime(chat.UpdatedAt),
			chat.UnreadCount,
			chat.Type,
			truncateText(name, 36),
			chat.JID,
		)
	}
	_ = w.Flush()
}

func printMessageTable(messages []cliMessage, chatJID string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "WHEN\tFROM\tTYPE\tMESSAGE")
	for _, msg := range messages {
		body := messageSummary(msg)
		fmt.Fprintf(
			w, "%s\t%s\t%s\t%s\n",
			formatCLITime(msg.Timestamp),
			messageSenderLabel(msg, chatJID),
			msg.Type,
			truncateText(body, 96),
		)
	}
	_ = w.Flush()
}

func isAuxiliaryChat(jid string) bool {
	return strings.HasSuffix(jid, "@newsletter") || jid == "status@broadcast"
}

func messageSummary(msg cliMessage) string {
	if msg.Body != nil && strings.TrimSpace(*msg.Body) != "" {
		return strings.ReplaceAll(strings.TrimSpace(*msg.Body), "\n", " ")
	}
	if msg.MediaPath != nil && *msg.MediaPath != "" {
		return "[media] " + *msg.MediaPath
	}
	if msg.MediaID != nil && *msg.MediaID != "" {
		return "[media] " + *msg.MediaID
	}
	return ""
}

func messageSenderLabel(msg cliMessage, chatJID string) string {
	if msg.Status != nil && *msg.Status != "received" {
		return "me"
	}
	if msg.SenderJID == "" {
		return "them"
	}
	if strings.HasSuffix(chatJID, "@g.us") {
		return shortJID(msg.SenderJID)
	}
	return "them"
}

func formatCLITime(value string) string {
	if value == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	local := t.Local()
	now := time.Now()
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("15:04")
	}
	if local.Year() == now.Year() {
		return local.Format("02 Jan 15:04")
	}
	return local.Format("2006-01-02")
}

func truncateText(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	if max <= 1 {
		return value[:max]
	}
	return value[:max-1] + "..."
}

func shortJID(jid string) string {
	if left, _, ok := strings.Cut(jid, "@"); ok {
		return left
	}
	return jid
}
