package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
)

type cliDevicePage struct {
	Items []cliDevice `json:"items"`
}

type cliDevice struct {
	JID      string `json:"jid"`
	Platform string `json:"platform"`
}

func runDevices(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "revoke" {
		return runDeviceRevoke(ctx, args[1:])
	}
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	body, err := httpGetAuth(ctx, *addr+"/session/devices", token)
	if err != nil {
		return err
	}
	if *jsonOut {
		printJSONBody(body)
		return nil
	}
	var page cliDevicePage
	if err := json.Unmarshal(body, &page); err != nil {
		return fmt.Errorf("decode devices response: %w", err)
	}
	printDeviceTable(page.Items)
	return nil
}

func runDeviceRevoke(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("devices revoke", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: usage: devices revoke <jid>", errInvalidInput)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	jid := fs.Arg(0)
	req, err := httpNewAuthRequest(ctx, "DELETE", fmt.Sprintf("%s/session/devices/%s", *addr, jid), token, nil)
	if err != nil {
		return err
	}
	if _, err := doRequest(req); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "device %s revoked\n", jid)
	return nil
}

func printDeviceTable(devices []cliDevice) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PLATFORM\tJID")
	for _, device := range devices {
		platform := device.Platform
		if platform == "" {
			platform = "-"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\n", platform, device.JID)
	}
	_ = w.Flush()
}
