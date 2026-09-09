package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/backup"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/service"
)

func TestBackupService_CreateAndRetrieve(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "wa_backup_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	fileStore, err := backup.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to init file store: %v", err)
	}

	mockCli := &MockClient{connected: true, loggedIn: true}
	mockStore := &MockStore{
		messages: []domain.ChatMessage{
			{
				ID:        "M1",
				ChatJID:   "123456@g.us",
				SenderJID: "user1@s.whatsapp.net",
				Timestamp: time.Now().Add(-10 * time.Minute),
				Text:      "Halo selamat pagi semuanya",
			},
			{
				ID:        "M2",
				ChatJID:   "123456@g.us",
				SenderJID: "user2@s.whatsapp.net",
				Timestamp: time.Now().Add(-5 * time.Minute),
				Text:      "Ini foto lampiran rapat",
				HasMedia:  true,
				MediaType: "image",
			},
		},
	}

	backupSvc := service.NewBackupService(mockCli, mockStore, fileStore)
	ctx := context.Background()

	result, err := backupSvc.BackupGroupChat(ctx, domain.BackupRequest{
		ChatJID:      "123456@g.us",
		IncludeMedia: true,
		Limit:        100,
	})
	if err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	if result.TotalMessages != 2 {
		t.Errorf("expected 2 messages, got %d", result.TotalMessages)
	}
	if result.TotalMedia != 1 {
		t.Errorf("expected 1 media message, got %d", result.TotalMedia)
	}

	// Verify reading backup back
	meta, data, err := backupSvc.GetBackup(ctx, result.BackupID)
	if err != nil {
		t.Fatalf("get backup failed: %v", err)
	}

	if meta.BackupID != result.BackupID {
		t.Errorf("expected backup ID %s, got %s", result.BackupID, meta.BackupID)
	}

	if len(data) == 0 {
		t.Errorf("expected non-empty backup archive payload")
	}

	// Verify list backups
	list, err := backupSvc.ListBackups(ctx)
	if err != nil {
		t.Fatalf("list backups failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 backup listed, got %d", len(list))
	}
}
