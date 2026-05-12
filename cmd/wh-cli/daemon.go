package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/l-i408/wh-cli/internal/api"
	"github.com/l-i408/wh-cli/internal/auth"
	"github.com/l-i408/wh-cli/internal/crypto"
	"github.com/l-i408/wh-cli/internal/keyring"
	"github.com/l-i408/wh-cli/internal/store"
	"github.com/l-i408/wh-cli/internal/wa"
	"github.com/l-i408/wh-cli/internal/ws"
)

func runDaemon(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:7777", "listen address")
	dbPath := fs.String("db", defaultDBPath(), "database path")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}

	if err := validateListenAddress(*listen); err != nil {
		return err
	}

	deps, cleanup, err := buildDaemonDependencies(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer cleanup()

	srv := &http.Server{
		Addr:              *listen,
		Handler:           api.NewRouter(deps),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Default().Error("daemon shutdown failed", "error", err)
		}
	}()

	slog.Default().Info("daemon listening", "addr", *listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve daemon: %w", err)
	}
	return nil
}

func buildDaemonDependencies(ctx context.Context, dbPath string) (api.Dependencies, func(), error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return api.Dependencies{}, func() {}, fmt.Errorf("create data dir: %w", err)
	}
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		return api.Dependencies{}, func() {}, err
	}
	cleanup := func() {
		if err := db.Close(); err != nil {
			slog.Default().Error("close database failed", "error", err)
		}
	}
	if err := db.ApplyInitialSchema(ctx); err != nil {
		cleanup()
		return api.Dependencies{}, func() {}, err
	}

	secret, err := loadOrCreateJWTSecret(ctx)
	if err != nil {
		cleanup()
		return api.Dependencies{}, func() {}, err
	}
	dataDir := filepath.Dir(dbPath)
	sessionPath := filepath.Join(dataDir, "whatsmeow-session.db")
	mediaDir := filepath.Join(dataDir, "media")
	messageRepo := store.NewMessageRepo(db)
	contactRepo := store.NewContactRepo(db)
	groupRepo := store.NewGroupRepo(db)
	mediaRepo := store.NewMediaRepo(db)
	auditRepo := store.NewAuditRepo(db)
	hub := ws.NewHub()
	messageSink := &eventingMessageSink{repo: messageRepo, hub: hub}
	deviceSink := &eventingDeviceSink{hub: hub, audit: auditRepo}
	session, err := wa.NewWhatsmeowSession(ctx, sessionPath, messageSink, groupRepo, contactRepo, deviceSink, mediaRepo, mediaDir)
	if err != nil {
		cleanup()
		return api.Dependencies{}, func() {}, err
	}
	if err := session.ConnectExisting(); err != nil {
		slog.Default().Warn("existing WhatsApp session did not connect", "error", err)
	}
	if ownJID := session.SelfJID(); ownJID != "" {
		if err := messageRepo.BackfillMissingSenderJID(ctx, ownJID); err != nil {
			slog.Default().Warn("initial sender jid backfill failed", "error", err)
		}
	}
	go func() {
		if err := session.SyncAllContacts(ctx); err != nil {
			slog.Default().Warn("initial contact sync failed", "error", err)
		}
	}()
	go func() {
		if _, err := session.RefreshGroups(ctx); err != nil {
			slog.Default().Warn("initial group sync failed", "error", err)
		}
	}()

	passphraseHash, err := localPassphraseHash(ctx)
	if err != nil {
		cleanup()
		return api.Dependencies{}, func() {}, err
	}
	authSvc := auth.NewService(secret, passphraseHash, auth.NewTokenStore(db), time.Now)
	adminSvc := &daemonAdminService{dbPath: dbPath, sessionPath: sessionPath, mediaDir: mediaDir, session: session, auth: authSvc}
	return api.Dependencies{
		Auth:     authSvc,
		Session:  session,
		Sender:   session,
		GroupsWA: session,
		History:  session,
		Devices:  session,
		Admin:    adminSvc,
		Audit:    auditRepo,
		Messages: messageRepo,
		Contacts: contactRepo,
		Groups:   groupRepo,
		Media:    mediaRepo,
		MediaDir: mediaDir,
		Hub:      hub,
		Unlock:   keyring.NewUnlockCache(time.Now),
	}, cleanup, nil
}

type eventingMessageSink struct {
	repo *store.MessageRepo
	hub  *ws.Hub
}

func (s *eventingMessageSink) SaveText(ctx context.Context, msg store.Message, displayName string) error {
	if err := s.repo.SaveText(ctx, msg, displayName); err != nil {
		return err
	}
	eventMsg := msg
	eventChatJID, err := s.repo.CanonicalChatJID(ctx, msg.ChatJID)
	if err != nil {
		eventChatJID = msg.ChatJID
	}
	eventMsg.ChatJID = eventChatJID
	if s.hub != nil {
		s.hub.Publish(ctx, ws.Event{
			ID:      eventMsg.ID,
			Type:    ws.EventMessageNew,
			Time:    eventMsg.Timestamp,
			ChatJID: eventChatJID,
			Payload: map[string]any{"message": eventMsg},
		})
		s.hub.Publish(ctx, ws.Event{
			ID:      eventChatJID + ":" + eventMsg.ID,
			Type:    ws.EventChatUpdated,
			Time:    eventMsg.Timestamp,
			ChatJID: eventChatJID,
			Payload: map[string]any{"last_message_id": eventMsg.ID},
		})
	}
	return nil
}

func (s *eventingMessageSink) SaveHistoricalText(ctx context.Context, msg store.Message, displayName string) error {
	return s.repo.SaveText(ctx, msg, displayName)
}

func (s *eventingMessageSink) UpdateStatus(ctx context.Context, messageID string, status string) error {
	return s.repo.UpdateStatus(ctx, messageID, status)
}

func (s *eventingMessageSink) BackfillMissingSenderJID(ctx context.Context, ownJID string) error {
	return s.repo.BackfillMissingSenderJID(ctx, ownJID)
}

type eventingDeviceSink struct {
	hub   *ws.Hub
	audit *store.AuditRepo
}

func (s *eventingDeviceSink) OnDeviceLinked(ctx context.Context, jid string, platform string, ts time.Time) {
	if s.hub != nil {
		s.hub.Publish(ctx, ws.Event{
			ID:      jid,
			Type:    ws.EventSessionDeviceLinked,
			Time:    ts,
			Payload: map[string]any{"jid": jid, "platform": platform},
		})
	}
	if s.audit != nil {
		_ = s.audit.Log(ctx, "system", "session.device_linked", jid, "detected", "high")
	}
	slog.Default().Warn("new linked device detected", "jid", jid, "platform", platform)
}

type daemonAdminService struct {
	dbPath      string
	sessionPath string
	mediaDir    string
	session     interface {
		Logout(ctx context.Context) error
	}
	auth interface {
		RevokeAll(ctx context.Context) error
	}
}

func (s *daemonAdminService) ExportDB(ctx context.Context) ([]byte, error) {
	data, err := os.ReadFile(s.dbPath)
	if err != nil {
		return nil, fmt.Errorf("read db for export: %w", err)
	}
	return data, nil
}

func (s *daemonAdminService) WipeAll(ctx context.Context) error {
	if s.session != nil {
		_ = s.session.Logout(ctx)
	}
	if s.auth != nil {
		_ = s.auth.RevokeAll(ctx)
	}
	if err := crypto.ZeroFile(s.dbPath); err != nil {
		return fmt.Errorf("wipe db: %w", err)
	}
	if err := crypto.ZeroFile(s.sessionPath); err != nil {
		return fmt.Errorf("wipe session: %w", err)
	}
	if err := os.RemoveAll(s.mediaDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("wipe media: %w", err)
	}
	return nil
}

func loadOrCreateJWTSecret(ctx context.Context) ([]byte, error) {
	kr := keyring.NewOSStore()
	secret, err := kr.Get(ctx, keyring.AccountJWTSecret)
	if err == nil && len(secret) >= 32 {
		return secret, nil
	}
	secret, err = auth.GenerateSecret()
	if err != nil {
		return nil, err
	}
	if err := kr.Set(ctx, keyring.AccountJWTSecret, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func localPassphraseHash(ctx context.Context) ([]byte, error) {
	kr := keyring.NewOSStore()
	hash, err := kr.Get(ctx, keyring.AccountLocalPassphraseHash)
	if err == nil && len(hash) > 0 {
		return hash, nil
	}
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return nil, err
	}
	hash, err = auth.GenerateSecret()
	if err != nil {
		return nil, fmt.Errorf("generate local passphrase hash: %w", err)
	}
	if err := kr.Set(ctx, keyring.AccountLocalPassphraseHash, hash); err != nil {
		return nil, fmt.Errorf("store local passphrase hash: %w", err)
	}
	return hash, nil
}

func defaultDBPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "wh-cli.db")
	}
	return filepath.Join(dir, "wh-cli", "wh-cli.db")
}

func validateListenAddress(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: listen must be host:port", errInvalidInput)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("%w: non-local listen requires the future TLS opt-in path", errInvalidInput)
	}
	return nil
}
