package ports

import (
	"context"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
)

// SessionService defines the inbound driving port for WhatsApp session lifecycle operations.
type SessionService interface {
	Start(ctx context.Context) error
	GetStatus(ctx context.Context) (domain.SessionStatus, error)
	RequestPairing(ctx context.Context, req domain.PairingRequest) (string, error)
	Disconnect(ctx context.Context) error
	SupervisorLoop(ctx context.Context)
}

// WhatsAppService defines the inbound driving port for interacting with WhatsApp messaging & data.
type WhatsAppService interface {
	SendText(ctx context.Context, msg domain.TextMessage) (string, error)
	SendMedia(ctx context.Context, msg domain.MediaMessage) (string, error)
	ListGroups(ctx context.Context) ([]domain.GroupInfo, error)
	GetGroupInfo(ctx context.Context, groupJID string) (domain.GroupInfo, error)
	DownloadMedia(ctx context.Context, info domain.MediaDownloadInfo) ([]byte, string, error)
	GetRecentMessages(ctx context.Context, chatJID string, limit int) ([]domain.ChatMessage, error)
}

// BackupService defines the inbound driving port for creating and managing chat backups.
type BackupService interface {
	BackupGroupChat(ctx context.Context, req domain.BackupRequest) (domain.BackupResult, error)
	GetBackup(ctx context.Context, backupID string) (domain.BackupResult, []byte, error)
	ListBackups(ctx context.Context) ([]domain.BackupResult, error)
}
