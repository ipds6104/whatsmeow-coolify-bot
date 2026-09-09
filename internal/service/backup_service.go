package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

// BackupServiceImpl orchestrates group chat data extraction and archive generation.
type BackupServiceImpl struct {
	client ports.WhatsAppClientPort
	store  ports.SessionStorePort
	backup ports.BackupStorePort
}

var _ ports.BackupService = (*BackupServiceImpl)(nil)

func NewBackupService(
	client ports.WhatsAppClientPort,
	store ports.SessionStorePort,
	backup ports.BackupStorePort,
) *BackupServiceImpl {
	return &BackupServiceImpl{
		client: client,
		store:  store,
		backup: backup,
	}
}

type GroupChatArchive struct {
	Group     domain.GroupInfo     `json:"group"`
	ExportedAt time.Time           `json:"exported_at"`
	Messages  []domain.ChatMessage `json:"messages"`
}

func (b *BackupServiceImpl) BackupGroupChat(ctx context.Context, req domain.BackupRequest) (domain.BackupResult, error) {
	if req.ChatJID == "" {
		return domain.BackupResult{}, domain.ErrInvalidRecipient
	}

	// 1. Fetch group metadata
	var groupInfo domain.GroupInfo
	var err error
	if b.client.IsConnected() {
		groupInfo, err = b.client.GetGroupInfo(ctx, req.ChatJID)
		if err != nil {
			groupInfo = domain.GroupInfo{JID: req.ChatJID, Name: "Unknown Group (" + req.ChatJID + ")"}
		}
	} else {
		groupInfo = domain.GroupInfo{JID: req.ChatJID, Name: req.ChatJID}
	}

	// 2. Fetch messages from database
	messages, err := b.store.GetMessages(ctx, req.ChatJID, req.Limit)
	if err != nil {
		return domain.BackupResult{}, fmt.Errorf("failed to load chat messages for backup: %w", err)
	}

	// 3. Count media
	mediaCount := 0
	for _, m := range messages {
		if m.HasMedia {
			mediaCount++
		}
	}

	format := req.Format
	if format == "" {
		format = "json"
	}

	archive := GroupChatArchive{
		Group:      groupInfo,
		ExportedAt: time.Now().UTC(),
		Messages:   messages,
	}

	data, err := json.MarshalIndent(archive, "", "  ")
	if err != nil {
		return domain.BackupResult{}, fmt.Errorf("failed to serialize chat archive: %w", err)
	}

	backupID := fmt.Sprintf("backup_%s_%d", req.ChatJID, time.Now().Unix())
	result := domain.BackupResult{
		BackupID:      backupID,
		ChatJID:       req.ChatJID,
		ChatName:      groupInfo.Name,
		TotalMessages: len(messages),
		TotalMedia:    mediaCount,
		CreatedAt:     time.Now().UTC(),
		Format:        format,
		FileSize:      int64(len(data)),
	}

	if err := b.backup.SaveBackup(ctx, result, data); err != nil {
		return domain.BackupResult{}, fmt.Errorf("failed to persist backup: %w", err)
	}

	return result, nil
}

func (b *BackupServiceImpl) GetBackup(ctx context.Context, backupID string) (domain.BackupResult, []byte, error) {
	return b.backup.ReadBackup(ctx, backupID)
}

func (b *BackupServiceImpl) ListBackups(ctx context.Context) ([]domain.BackupResult, error) {
	return b.backup.ListBackups(ctx)
}
