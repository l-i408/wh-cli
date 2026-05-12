package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func runSetup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	wait := fs.Duration("wait", 60*time.Second, "maximum time to wait for WhatsApp connection")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}

	if err := ensureDaemonRunning(ctx, *addr); err != nil {
		return err
	}
	if err := ensureCLILogin(ctx, *addr); err != nil {
		return err
	}
	status, err := sessionStatus(ctx, *addr)
	if err != nil {
		return err
	}
	if status == "connected" {
		_, _ = fmt.Fprintln(os.Stdout, "WhatsApp session is connected.")
		_, _ = fmt.Fprintln(os.Stdout, "Try: wh-cli chats")
		return nil
	}

	_, _ = fmt.Fprintln(os.Stdout, "Scan this QR with WhatsApp > Linked devices:")
	if err := runQR(ctx, []string{"--addr", *addr}); err != nil {
		return err
	}
	deadline := time.Now().Add(*wait)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		status, err = sessionStatus(ctx, *addr)
		if err == nil && status == "connected" {
			_, _ = fmt.Fprintln(os.Stdout, "WhatsApp session connected.")
			_, _ = fmt.Fprintln(os.Stdout, "Try: wh-cli chats")
			return nil
		}
	}
	return fmt.Errorf("%w: QR was shown but session did not connect before timeout", errDaemonUnavailable)
}

func ensureDaemonRunning(ctx context.Context, addr string) error {
	if err := healthCheck(ctx, addr); err == nil {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	logDir := defaultSetupLogDir()
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return fmt.Errorf("create setup log dir: %w", err)
	}
	outPath := filepath.Join(logDir, "daemon.out.log")
	errPath := filepath.Join(logDir, "daemon.err.log")
	// #nosec G304 -- path is under the wh-cli setup log directory.
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon stdout log: %w", err)
	}
	// #nosec G304 -- path is under the wh-cli setup log directory.
	errFile, err := os.OpenFile(errPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		_ = out.Close()
		return fmt.Errorf("open daemon stderr log: %w", err)
	}
	// #nosec G204 -- exe is this wh-cli binary resolved from os.Executable.
	cmd := exec.CommandContext(context.Background(), exe, "daemon")
	cmd.Stdout = out
	cmd.Stderr = errFile
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = hiddenProcessAttrs()
	}
	if err := cmd.Start(); err != nil {
		_ = out.Close()
		_ = errFile.Close()
		return fmt.Errorf("start daemon: %w", err)
	}
	go func() {
		_ = cmd.Wait()
		_ = out.Close()
		_ = errFile.Close()
	}()
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if err := healthCheck(ctx, addr); err == nil {
			return nil
		}
	}
	return fmt.Errorf("%w: daemon did not become ready", errDaemonUnavailable)
}

func ensureCLILogin(ctx context.Context, addr string) error {
	if _, err := cliAccessToken(ctx); err == nil {
		return nil
	}
	hash, err := localPassphraseHash(ctx)
	if err != nil {
		return err
	}
	if err := runLogin(ctx, []string{"--addr", addr, "--passphrase-hash", string(hash)}); err != nil {
		return err
	}
	return nil
}

func healthCheck(ctx context.Context, addr string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(resp.Status)
	}
	return nil
}

func defaultSetupLogDir() string {
	if dir := os.Getenv("APPDATA"); dir != "" {
		return filepath.Join(dir, "wh-cli", "logs")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "state", "wh-cli", "logs")
	}
	return "."
}
