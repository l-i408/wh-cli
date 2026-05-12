package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/l-i408/wh-cli/internal/store"
)

// PrepareLocalFile copies a local file into the content-addressed media store.
func PrepareLocalFile(ctx context.Context, repo *store.MediaRepo, mediaDir string, srcPath string) (store.MediaBlob, []byte, error) {
	data, err := os.ReadFile(srcPath) // #nosec G304 -- wh send accepts an explicit local path from the CLI user.
	if err != nil {
		return store.MediaBlob{}, nil, fmt.Errorf("read media file: %w", err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	ext := filepath.Ext(srcPath)
	dstPath := filepath.Join(mediaDir, hash+ext)
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		return store.MediaBlob{}, nil, fmt.Errorf("create media dir: %w", err)
	}
	if err := ensureWithinDir(mediaDir, dstPath); err != nil {
		return store.MediaBlob{}, nil, err
	}
	if _, err := os.Stat(dstPath); err != nil { // #nosec G703 -- dstPath is content-addressed and checked inside mediaDir.
		if !os.IsNotExist(err) {
			return store.MediaBlob{}, nil, fmt.Errorf("stat media dst: %w", err)
		}
		if err := copyFile(dstPath, srcPath); err != nil {
			return store.MediaBlob{}, nil, err
		}
	}
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	mimeType = normalizeMIME(ext, mimeType)
	blob := store.MediaBlob{
		ID:         hash,
		MIME:       mimeType,
		Size:       int64(len(data)),
		SHA256:     hash,
		LocalPath:  dstPath,
		Downloaded: true,
	}
	if err := repo.Save(ctx, blob); err != nil {
		return store.MediaBlob{}, nil, err
	}
	return blob, data, nil
}

func normalizeMIME(ext string, detected string) string {
	switch strings.ToLower(ext) {
	case ".ogg", ".opus":
		return "audio/ogg; codecs=opus"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	default:
		return detected
	}
}

func ensureWithinDir(baseDir string, path string) error {
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("resolve media dir: %w", err)
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve media dst: %w", err)
	}
	rel, err := filepath.Rel(baseAbs, pathAbs)
	if err != nil {
		return fmt.Errorf("rel media dst: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("media dst escapes media dir")
	}
	return nil
}

func copyFile(dst string, src string) error {
	in, err := os.Open(src) // #nosec G304 -- wh send accepts an explicit local path from the CLI user.
	if err != nil {
		return fmt.Errorf("open media src: %w", err)
	}
	defer func() {
		_ = in.Close()
	}()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 G703 -- dst is content-addressed and checked inside mediaDir by caller.
	if err != nil {
		return fmt.Errorf("create media dst: %w", err)
	}
	defer func() {
		_ = out.Close()
	}()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy media: %w", err)
	}
	return nil
}
