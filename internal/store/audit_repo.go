package store

import (
	"context"
	"fmt"
	"time"
)

// AuditRepo writes security-relevant events to the audit_log table.
type AuditRepo struct {
	db *DB
}

// NewAuditRepo constructs an audit repository.
func NewAuditRepo(db *DB) *AuditRepo {
	return &AuditRepo{db: db}
}

// Log records an audit event. Severity: "info", "warn", "high".
func (r *AuditRepo) Log(ctx context.Context, actor, action, target, result, severity string) error {
	_, err := r.db.Exec(ctx, `
INSERT INTO audit_log (ts, actor, action, target, result, severity)
VALUES (?, ?, ?, ?, ?, ?)
`, time.Now().UTC().Format(time.RFC3339), actor, action, target, result, severity)
	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}
