package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/l-i408/wh-cli/internal/store"
)

// SaveDownloaded stores bytes received from WhatsApp in the content-addressed media store.
func SaveDownloaded(ctx context.Context, repo *store.MediaRepo, mediaDir string, messageID string, data []byte, mimeType string, filename string) (store.MediaBlob, error) {
	if len(data) == 0 {
		return store.MediaBlob{}, fmt.Errorf("empty media payload")
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	ext := mediaExtension(mimeType, filename)
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	dstPath := filepath.Join(mediaDir, hash+ext)
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		return store.MediaBlob{}, fmt.Errorf("create media dir: %w", err)
	}
	if err := ensureWithinDir(mediaDir, dstPath); err != nil {
		return store.MediaBlob{}, err
	}
	if _, err := os.Stat(dstPath); err != nil {
		if !os.IsNotExist(err) {
			return store.MediaBlob{}, fmt.Errorf("stat media dst: %w", err)
		}
		if err := os.WriteFile(dstPath, data, 0o600); err != nil {
			return store.MediaBlob{}, fmt.Errorf("write media dst: %w", err)
		}
	}
	blob := store.MediaBlob{
		ID:         hash,
		MessageID:  messageID,
		MIME:       mimeType,
		Size:       int64(len(data)),
		SHA256:     hash,
		LocalPath:  dstPath,
		Downloaded: true,
	}
	if err := repo.Save(ctx, blob); err != nil {
		return store.MediaBlob{}, err
	}
	return blob, nil
}

func mediaExtension(mimeType string, filename string) string {
	if ext := filepath.Ext(filename); ext != "" {
		return ext
	}
	if exts, err := mime.ExtensionsByType(mimeType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	switch {
	case strings.HasPrefix(mimeType, "image/jpeg"):
		return ".jpg"
	case strings.HasPrefix(mimeType, "image/png"):
		return ".png"
	case strings.HasPrefix(mimeType, "audio/ogg"):
		return ".ogg"
	case strings.HasPrefix(mimeType, "audio/mpeg"):
		return ".mp3"
	default:
		return ".bin"
	}
}
