package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/l-i408/wh-cli/internal/store"
)

func TestPrepareLocalFileDeduplicatesBySHA256(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(ctx); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	dir := t.TempDir()
	first := filepath.Join(dir, "first.txt")
	second := filepath.Join(dir, "second.txt")
	if err := os.WriteFile(first, []byte("same payload"), 0o600); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := os.WriteFile(second, []byte("same payload"), 0o600); err != nil {
		t.Fatalf("write second: %v", err)
	}

	repo := store.NewMediaRepo(db)
	mediaDir := filepath.Join(t.TempDir(), "media")
	firstBlob, firstData, err := PrepareLocalFile(ctx, repo, mediaDir, first)
	if err != nil {
		t.Fatalf("prepare first: %v", err)
	}
	secondBlob, secondData, err := PrepareLocalFile(ctx, repo, mediaDir, second)
	if err != nil {
		t.Fatalf("prepare second: %v", err)
	}

	if string(firstData) != "same payload" || string(secondData) != "same payload" {
		t.Fatal("unexpected media data")
	}
	if firstBlob.SHA256 != secondBlob.SHA256 {
		t.Fatalf("sha mismatch: %s != %s", firstBlob.SHA256, secondBlob.SHA256)
	}
	if firstBlob.LocalPath != secondBlob.LocalPath {
		t.Fatalf("expected deduped local path, got %q and %q", firstBlob.LocalPath, secondBlob.LocalPath)
	}
	if _, err := os.Stat(firstBlob.LocalPath); err != nil {
		t.Fatalf("stat stored media: %v", err)
	}
}

func TestSaveDownloadedStoresMediaFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "downloaded.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if err := db.ApplyInitialSchema(ctx); err != nil {
		t.Fatalf("ApplyInitialSchema returned error: %v", err)
	}
	repo := store.NewMediaRepo(db)
	mediaDir := filepath.Join(t.TempDir(), "media")

	blob, err := SaveDownloaded(ctx, repo, mediaDir, "msg-1", []byte("\x89PNG\r\n\x1a\npayload"), "image/png", "")
	if err != nil {
		t.Fatalf("SaveDownloaded returned error: %v", err)
	}
	if blob.MessageID != "msg-1" || blob.MIME != "image/png" || !blob.Downloaded {
		t.Fatalf("blob = %+v, want downloaded png for msg-1", blob)
	}
	if filepath.Ext(blob.LocalPath) != ".png" {
		t.Fatalf("extension = %q, want .png", filepath.Ext(blob.LocalPath))
	}
	if _, err := os.Stat(blob.LocalPath); err != nil {
		t.Fatalf("downloaded file missing: %v", err)
	}
}
