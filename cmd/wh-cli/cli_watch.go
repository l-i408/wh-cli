package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/coder/websocket"
)

func runWatch(ctx context.Context, args []string) error {
	addr := defaultDaemonAddr
	if len(args) == 2 && args[0] == "--addr" {
		addr = args[1]
	} else if len(args) != 0 {
		return fmt.Errorf("%w: usage watch [--addr URL]", errInvalidInput)
	}
	wsURL, err := websocketURL(addr)
	if err != nil {
		return err
	}
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", errDaemonUnavailable, err)
	}
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read websocket: %w", err)
		}
		fmt.Println(string(data))
	}
}

func websocketURL(addr string) (string, error) {
	parsed, err := url.Parse(addr)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", fmt.Errorf("%w: unsupported URL scheme", errInvalidInput)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/ws"
	return parsed.String(), nil
}
