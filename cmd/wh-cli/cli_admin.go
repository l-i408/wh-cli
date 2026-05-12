package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/l-i408/wh-cli/internal/auth"
	"github.com/l-i408/wh-cli/internal/crypto"
	"github.com/l-i408/wh-cli/internal/keyring"
)

func runRotateJWT(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rotate-jwt-secret", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	fmt.Println("This will invalidate ALL active tokens and force re-login everywhere.")
	if !confirm("Type YES to continue: ") {
		fmt.Println("aborted")
		return nil
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	body, err := httpPostJSON(ctx, *addr+"/admin/rotate-jwt", token, map[string]string{})
	if err != nil {
		return err
	}
	printJSONBody(body)

	// Store new secret in keyring (daemon also updates internally via API).
	newSecret, err := auth.GenerateSecret()
	if err != nil {
		return fmt.Errorf("generate local secret: %w", err)
	}
	kr := keyring.NewOSStore()
	if err := kr.Set(ctx, keyring.AccountJWTSecret, newSecret); err != nil {
		return fmt.Errorf("update keyring jwt secret: %w", err)
	}
	fmt.Println("JWT secret rotated. Re-login required.")
	return nil
}

func runWipe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("wipe", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	fmt.Println("WARNING: This will permanently delete ALL local data:")
	fmt.Println("  - Message history, contacts, groups")
	fmt.Println("  - Media files")
	fmt.Println("  - WhatsApp session (you will need to re-scan QR)")
	fmt.Println("  - All tokens and keyring entries")
	fmt.Println()
	if !confirm("Type WIPE to confirm: ") {
		fmt.Println("aborted")
		return nil
	}
	if !confirm("Are you sure? Type YES to proceed: ") {
		fmt.Println("aborted")
		return nil
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	if _, err := httpPostJSON(ctx, *addr+"/admin/wipe", token, map[string]string{}); err != nil {
		return err
	}
	kr := keyring.NewOSStore()
	_ = kr.Delete(ctx, keyring.AccountAccessToken)
	_ = kr.Delete(ctx, keyring.AccountRefreshToken)
	_ = kr.Delete(ctx, keyring.AccountJWTSecret)
	_ = kr.Delete(ctx, keyring.AccountMasterKey)
	_ = kr.Delete(ctx, keyring.AccountLocalPassphraseHash)
	fmt.Println("wipe complete — all local data deleted")
	return nil
}

func runExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	out := fs.String("out", "", "output file path (required)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if *out == "" {
		return fmt.Errorf("%w: --out is required", errInvalidInput)
	}
	fmt.Print("Export passphrase (will encrypt the backup): ")
	passphrase, err := readSecretLine()
	if err != nil {
		return fmt.Errorf("read passphrase: %w", err)
	}
	if passphrase == "" {
		return fmt.Errorf("%w: passphrase cannot be empty", errInvalidInput)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	plaintext, err := httpGetAuth(ctx, *addr+"/admin/export", token)
	if err != nil {
		return err
	}
	encrypted, err := crypto.EncryptExport([]byte(passphrase), plaintext)
	if err != nil {
		return fmt.Errorf("encrypt export: %w", err)
	}
	if err := os.WriteFile(*out, encrypted, 0o600); err != nil {
		return fmt.Errorf("write export file: %w", err)
	}
	fmt.Printf("export written to %s (%d bytes)\n", *out, len(encrypted))
	return nil
}

func runImport(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	in := fs.String("in", "", "input file path (required)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if *in == "" {
		return fmt.Errorf("%w: --in is required", errInvalidInput)
	}
	fmt.Print("Export passphrase: ")
	passphrase, err := readSecretLine()
	if err != nil {
		return fmt.Errorf("read passphrase: %w", err)
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		return fmt.Errorf("read import file: %w", err)
	}
	if _, err := crypto.DecryptExport([]byte(passphrase), data); err != nil {
		return fmt.Errorf("decrypt import: %w", err)
	}
	fmt.Println("Import validated. Stop the daemon, replace the DB file manually, then restart.")
	fmt.Println("Automated import requires a daemon restart — not yet implemented.")
	return nil
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(scanner.Text())
		return answer == "YES" || answer == "WIPE"
	}
	return false
}

func readSecretLine() (string, error) {
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}
