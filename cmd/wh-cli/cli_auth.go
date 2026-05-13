package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/l-i408/wh-cli/internal/auth"
	"github.com/l-i408/wh-cli/internal/keyring"
	"github.com/mdp/qrterminal/v3"
	"rsc.io/qr"
)

const defaultDaemonAddr = "http://127.0.0.1:7777"

func runStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	body, err := httpGet(ctx, *addr+"/session/status")
	if err != nil {
		return err
	}
	fmt.Println(string(body))
	return nil
}

func sessionStatus(ctx context.Context, addr string) (string, error) {
	body, err := httpGet(ctx, strings.TrimRight(addr, "/")+"/session/status")
	if err != nil {
		return "", err
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("decode session status: %w", err)
	}
	return resp.Status, nil
}

func runLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	passphraseHash := fs.String("passphrase-hash", "", "local passphrase hash")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if *passphraseHash == "" {
		return fmt.Errorf("%w: missing --passphrase-hash", errInvalidInput)
	}
	payload := map[string]string{"passphrase_hash": *passphraseHash, "client_label": "cli"}
	body, err := httpPostJSON(ctx, *addr+"/auth/login", "", payload)
	if err != nil {
		return err
	}
	var pair auth.TokenPair
	if err := json.Unmarshal(body, &pair); err != nil {
		return fmt.Errorf("decode login response: %w", err)
	}
	kr := keyring.NewOSStore()
	if err := kr.Set(ctx, keyring.AccountAccessToken, []byte(pair.AccessToken)); err != nil {
		return err
	}
	if err := kr.Set(ctx, keyring.AccountRefreshToken, []byte(pair.RefreshToken)); err != nil {
		return err
	}
	fmt.Println(string(body))
	return nil
}

func runLogout(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	kr := keyring.NewOSStore()
	token, err := kr.Get(ctx, keyring.AccountAccessToken)
	if err != nil {
		return errAuth
	}
	if _, err := httpPostJSON(ctx, *addr+"/auth/logout", string(token), map[string]string{}); err != nil {
		return err
	}
	_ = kr.Delete(ctx, keyring.AccountAccessToken)
	_ = kr.Delete(ctx, keyring.AccountRefreshToken)
	return nil
}

func runQR(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("qr", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	raw := fs.Bool("raw", false, "print raw SSE instead of rendering QR")
	pngPath := fs.String("png", "", "write QR as PNG file")
	urlOnly := fs.Bool("url", false, "print the live QR PNG URL")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if *urlOnly {
		url := strings.TrimRight(*addr, "/") + "/session/qr.png"
		fmt.Println(url)
		return nil
	}
	body, err := httpGet(ctx, *addr+"/session/qr")
	if err != nil {
		return err
	}
	if *raw {
		fmt.Print(string(body))
		return nil
	}
	code, ok := parseSSEData(string(body))
	if !ok {
		return fmt.Errorf("%w: qr response did not include data", errInvalidInput)
	}
	if *pngPath != "" {
		codePNG, err := qr.Encode(code, qr.L)
		if err != nil {
			return fmt.Errorf("encode qr png: %w", err)
		}
		if err := os.WriteFile(*pngPath, codePNG.PNG(), 0o600); err != nil {
			return fmt.Errorf("write qr png: %w", err)
		}
		fmt.Println(*pngPath)
		return nil
	}
	qrterminal.GenerateWithConfig(code, qrterminal.Config{
		Level:      qrterminal.L,
		Writer:     os.Stdout,
		HalfBlocks: true,
		QuietZone:  1,
	})
	return nil
}

func runPairCode(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pair-code", flag.ContinueOnError)
	addr := fs.String("addr", defaultDaemonAddr, "daemon address")
	phone := fs.String("phone", "", "phone number in international format without leading zero")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if *phone == "" {
		return fmt.Errorf("%w: missing --phone", errInvalidInput)
	}
	body, err := httpPostJSON(ctx, *addr+"/session/pair-code", "", map[string]string{"phone": *phone})
	if err != nil {
		return err
	}
	var resp struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("decode pair code response: %w", err)
	}
	fmt.Println(resp.Code)
	return nil
}

func parseSSEData(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		value, ok := strings.CutPrefix(line, "data:")
		if ok {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

func runUnlock(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("unlock", flag.ContinueOnError)
	ttl := fs.Duration("ttl", 30*time.Minute, "unlock TTL")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	if *ttl <= 0 {
		return fmt.Errorf("%w: ttl must be positive", errInvalidInput)
	}
	fmt.Printf("unlocked for %s\n", ttl.String())
	return nil
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	return doRequest(req)
}

func httpGetAuth(ctx context.Context, url string, bearer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	body, err := doRequest(req)
	if err == nil || bearer == "" || !errors.Is(err, errAuth) {
		return body, err
	}
	token, refreshErr := refreshCLITokens(ctx, daemonBaseURL(url))
	if refreshErr != nil {
		return nil, err
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return doRequest(req)
}

func httpNewAuthRequest(ctx context.Context, method string, url string, bearer string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	return req, nil
}

func httpPostJSON(ctx context.Context, url string, bearer string, payload any) ([]byte, error) {
	return httpJSON(ctx, http.MethodPost, url, bearer, payload)
}

func httpPatchJSON(ctx context.Context, url string, bearer string, payload any) ([]byte, error) {
	return httpJSON(ctx, http.MethodPatch, url, bearer, payload)
}

func httpJSON(ctx context.Context, method string, url string, bearer string, payload any) ([]byte, error) {
	return httpJSONRetry(ctx, method, url, bearer, payload, true)
}

func httpJSONRetry(ctx context.Context, method string, url string, bearer string, payload any, allowRefresh bool) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, &buf)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	body, err := doRequest(req)
	if err == nil || bearer == "" || !allowRefresh || !errors.Is(err, errAuth) {
		return body, err
	}
	token, refreshErr := refreshCLITokens(ctx, daemonBaseURL(url))
	if refreshErr != nil {
		return nil, err
	}
	return httpJSONRetry(ctx, method, url, token, payload, false)
}

func doRequest(req *http.Request) ([]byte, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errDaemonUnavailable, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		return body, nil
	case http.StatusUnauthorized:
		return nil, errAuth
	case http.StatusLocked:
		return nil, errLocked
	default:
		return nil, fmt.Errorf("daemon returned %s: %s", resp.Status, string(body))
	}
}

func refreshCLITokens(ctx context.Context, addr string) (string, error) {
	kr := keyring.NewOSStore()
	refreshToken, err := kr.Get(ctx, keyring.AccountRefreshToken)
	if err != nil {
		return "", errAuth
	}
	body, err := httpPostJSON(ctx, strings.TrimRight(addr, "/")+"/auth/refresh", "", map[string]string{
		"refresh_token": string(refreshToken),
	})
	if err != nil {
		return "", err
	}
	var pair auth.TokenPair
	if err := json.Unmarshal(body, &pair); err != nil {
		return "", fmt.Errorf("decode refresh response: %w", err)
	}
	if err := kr.Set(ctx, keyring.AccountAccessToken, []byte(pair.AccessToken)); err != nil {
		return "", err
	}
	if err := kr.Set(ctx, keyring.AccountRefreshToken, []byte(pair.RefreshToken)); err != nil {
		return "", err
	}
	return pair.AccessToken, nil
}

func daemonBaseURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return defaultDaemonAddr
	}
	return parsed.Scheme + "://" + parsed.Host
}
