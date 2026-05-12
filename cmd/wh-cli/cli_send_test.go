package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSendArgsAllowsFlagsAfterChatJID(t *testing.T) {
	t.Parallel()

	opts, err := parseSendArgs([]string{
		"278318410670272@lid",
		"--file", "C:\\tmp\\image.png",
		"--caption", "Prueba",
	})
	if err != nil {
		t.Fatalf("parseSendArgs returned error: %v", err)
	}
	if opts.chatJID != "278318410670272@lid" {
		t.Fatalf("chatJID = %q", opts.chatJID)
	}
	if opts.filePath != "C:\\tmp\\image.png" {
		t.Fatalf("filePath = %q", opts.filePath)
	}
	if opts.caption != "Prueba" {
		t.Fatalf("caption = %q", opts.caption)
	}
}

func TestParseSendArgsRejectsMissingCaptionValue(t *testing.T) {
	t.Parallel()

	_, err := parseSendArgs([]string{"278318410670272@lid", "--file", "C:\\tmp\\image.png", "--caption"})
	if !errors.Is(err, errInvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestParseSendArgsText(t *testing.T) {
	t.Parallel()

	opts, err := parseSendArgs([]string{"278318410670272@lid", "hola"})
	if err != nil {
		t.Fatalf("parseSendArgs returned error: %v", err)
	}
	if opts.text != "hola" {
		t.Fatalf("text = %q", opts.text)
	}
}

func TestNormalizeWindowsShellPath(t *testing.T) {
	t.Parallel()

	got := normalizeWindowsShellPath("/c/Users/onava/Desktop/file.png")
	want := "C:" + string(filepath.Separator) + filepath.Join("Users", "onava", "Desktop", "file.png")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestNormalizeAndVerifyMediaPath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write media: %v", err)
	}
	got, err := normalizeAndVerifyMediaPath(path)
	if err != nil {
		t.Fatalf("normalizeAndVerifyMediaPath returned error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("path is not absolute: %q", got)
	}
}
