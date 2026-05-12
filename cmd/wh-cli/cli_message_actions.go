package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
)

func runReact(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("react", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if fs.NArg() != 3 {
		return fmt.Errorf("%w: usage react <chat_jid> <message_id> <emoji>", errInvalidInput)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	target, err := resolveSingleTarget(ctx, *addr, token, fs.Arg(0), "chat")
	if err != nil {
		return err
	}
	payload := map[string]string{"chat_jid": target.JID, "emoji": fs.Arg(2)}
	if _, err := httpPostJSON(ctx, fmt.Sprintf("%s/messages/%s/react", *addr, fs.Arg(1)), token, payload); err != nil {
		return err
	}
	fmt.Println(`{"status":"accepted"}`)
	return nil
}

func runReply(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("reply", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if fs.NArg() != 3 {
		return fmt.Errorf("%w: usage reply <chat_jid> <message_id> <text>", errInvalidInput)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	target, err := resolveSingleTarget(ctx, *addr, token, fs.Arg(0), "chat")
	if err != nil {
		return err
	}
	payload := map[string]string{"chat_jid": target.JID, "type": "text", "body": fs.Arg(2)}
	body, err := httpPostJSON(ctx, fmt.Sprintf("%s/messages/%s/reply", *addr, fs.Arg(1)), token, payload)
	if err != nil {
		return err
	}
	printJSONBody(body)
	return nil
}

func runForward(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("forward", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if fs.NArg() < 2 {
		return fmt.Errorf("%w: usage forward <message_id> <target_chat_jid> [target_chat_jid...]", errInvalidInput)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	targets := make([]string, 0, fs.NArg()-1)
	for _, rawTarget := range fs.Args()[1:] {
		target, err := resolveSingleTarget(ctx, *addr, token, rawTarget, "chat")
		if err != nil {
			return err
		}
		targets = append(targets, target.JID)
	}
	payload := map[string][]string{"to_jids": targets}
	body, err := httpPostJSON(ctx, fmt.Sprintf("%s/messages/%s/forward", *addr, fs.Arg(0)), token, payload)
	if err != nil {
		return err
	}
	printJSONBody(body)
	return nil
}

func printJSONBody(body []byte) {
	var pretty map[string]any
	if err := json.Unmarshal(body, &pretty); err == nil {
		encoded, _ := json.Marshal(pretty)
		fmt.Println(string(encoded))
		return
	}
	fmt.Println(string(body))
}
