package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func runSend(ctx context.Context, args []string) error {
	opts, err := parseSendArgs(args)
	if err != nil {
		return err
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	target, err := resolveSingleTarget(ctx, opts.addr, token, opts.chatJID, "chat")
	if err != nil {
		return err
	}
	payload := map[string]string{"chat_jid": target.JID}
	switch {
	case opts.filePath != "":
		mediaPath, err := normalizeAndVerifyMediaPath(opts.filePath)
		if err != nil {
			return err
		}
		payload["file_path"] = opts.filePath
		payload["file_path"] = mediaPath
		payload["caption"] = opts.caption
	case opts.audioPath != "":
		mediaPath, err := normalizeAndVerifyMediaPath(opts.audioPath)
		if err != nil {
			return err
		}
		payload["audio_path"] = mediaPath
	default:
		payload["type"] = "text"
		payload["body"] = opts.text
	}
	body, err := httpPostJSON(ctx, opts.addr+"/messages", token, payload)
	if err != nil {
		return err
	}
	var pretty map[string]any
	if err := json.Unmarshal(body, &pretty); err == nil {
		encoded, _ := json.Marshal(pretty)
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Println(string(body))
	return nil
}

type sendOptions struct {
	addr      string
	chatJID   string
	text      string
	filePath  string
	audioPath string
	caption   string
}

func parseSendArgs(args []string) (sendOptions, error) {
	opts := sendOptions{addr: defaultDaemonAddr}
	positionals := make([]string, 0, 2)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--addr":
			value, ok := nextArg(args, &i)
			if !ok {
				return sendOptions{}, fmt.Errorf("%w: --addr requires a value", errInvalidInput)
			}
			opts.addr = value
		case "--file":
			value, ok := nextArg(args, &i)
			if !ok {
				return sendOptions{}, fmt.Errorf("%w: --file requires a value", errInvalidInput)
			}
			opts.filePath = value
		case "--audio":
			value, ok := nextArg(args, &i)
			if !ok {
				return sendOptions{}, fmt.Errorf("%w: --audio requires a value", errInvalidInput)
			}
			opts.audioPath = value
		case "--caption":
			value, ok := nextArg(args, &i)
			if !ok {
				return sendOptions{}, fmt.Errorf("%w: --caption requires a value", errInvalidInput)
			}
			opts.caption = value
		default:
			positionals = append(positionals, arg)
		}
	}
	if opts.filePath != "" || opts.audioPath != "" {
		if len(positionals) != 1 || (opts.filePath != "" && opts.audioPath != "") {
			return sendOptions{}, fmt.Errorf("%w: usage send <chat_jid> (--file <path> | --audio <path>) [--caption <text>]", errInvalidInput)
		}
		opts.chatJID = positionals[0]
		return opts, nil
	}
	if len(positionals) != 2 {
		return sendOptions{}, fmt.Errorf("%w: usage send <chat_jid> <text>", errInvalidInput)
	}
	opts.chatJID = positionals[0]
	opts.text = positionals[1]
	return opts, nil
}

func nextArg(args []string, index *int) (string, bool) {
	if *index+1 >= len(args) {
		return "", false
	}
	*index = *index + 1
	return args[*index], true
}

var msysPathPattern = regexp.MustCompile(`^/([a-zA-Z])/(.*)$`)

func normalizeAndVerifyMediaPath(path string) (string, error) {
	normalized := normalizeWindowsShellPath(path)
	absPath, err := filepath.Abs(normalized)
	if err != nil {
		return "", fmt.Errorf("%w: resolve media path: %w", errInvalidInput, err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("%w: media file is not readable: %w", errInvalidInput, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%w: media path is a directory", errInvalidInput)
	}
	return absPath, nil
}

func normalizeWindowsShellPath(path string) string {
	matches := msysPathPattern.FindStringSubmatch(path)
	if len(matches) != 3 {
		return path
	}
	drive := strings.ToUpper(matches[1])
	rest := strings.ReplaceAll(matches[2], "/", string(filepath.Separator))
	return drive + ":" + string(filepath.Separator) + rest
}
