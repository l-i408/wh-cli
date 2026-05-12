package store

import (
	"context"
	"fmt"
)

// MediaBlob describes a deduplicated media file.
type MediaBlob struct {
	ID         string `json:"id"`
	MessageID  string `json:"message_id,omitempty"`
	MIME       string `json:"mime"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	LocalPath  string `json:"local_path"`
	Downloaded bool   `json:"downloaded"`
}

// MediaRepo persists media metadata.
type MediaRepo struct {
	db *DB
}

// NewMediaRepo constructs a media repository.
func NewMediaRepo(db *DB) *MediaRepo {
	return &MediaRepo{db: db}
}

// Save records or updates a deduplicated media blob.
func (r *MediaRepo) Save(ctx context.Context, blob MediaBlob) error {
	_, err := r.db.Exec(ctx, `
INSERT INTO media_blobs (id, message_id, mime, size, sha256, local_path, downloaded)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(sha256) DO UPDATE SET message_id = excluded.message_id, local_path = excluded.local_path
`, blob.ID, blob.MessageID, blob.MIME, blob.Size, blob.SHA256, blob.LocalPath, blob.Downloaded)
	if err != nil {
		return fmt.Errorf("save media blob: %w", err)
	}
	return nil
}
