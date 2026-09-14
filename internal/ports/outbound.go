package ports

import (
	"context"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
)

// WhatsAppClientPort defines the outbound driven port for the low-level WhatsApp engine (whatsmeow).
type WhatsAppClientPort interface {
	Connect() error
	Disconnect()
	IsConnected() bool
	IsLoggedIn() bool
	GetDeviceJID() string
	GetDeviceLID() string
	PairPhone(ctx context.Context, phone string, clientDisplayName string) (string, error)
	SendTextMessage(ctx context.Context, to string, text string, replyID string, mentions []string) (string, error)
	SendMediaMessage(ctx context.Context, to string, media domain.MediaMessage) (string, error)
	DownloadMedia(ctx context.Context, info domain.MediaDownloadInfo) ([]byte, error)
	SendChatPresence(ctx context.Context, to string, state string) error
	GetGroupInfo(ctx context.Context, groupJID string) (domain.GroupInfo, error)
	GetJoinedGroups(ctx context.Context) ([]domain.GroupInfo, error)
	GetProfilePicture(ctx context.Context, jid string, preview bool) (*domain.ProfilePictureResult, error)
	SetProfilePicture(ctx context.Context, jid string, avatar []byte) (string, error)
	SetStatusMessage(ctx context.Context, status string) error
	SendStatusBroadcast(ctx context.Context, status domain.StatusBroadcastMessage) (string, error)
	RevokeMessage(ctx context.Context, chatJID string, messageID string) error
	ResetDevice(ctx context.Context) error
	AddEventHandler(handler func(evt interface{}))
}

// SessionStorePort defines the outbound port for persistent storage of messages and sessions.
type SessionStorePort interface {
	SaveMessage(ctx context.Context, msg domain.ChatMessage) error
	GetMessageByID(ctx context.Context, id string) (*domain.ChatMessage, error)
	GetMessages(ctx context.Context, chatJID string, limit int) ([]domain.ChatMessage, error)
	GetAllMessages(ctx context.Context, limit int) ([]domain.ChatMessage, error)
	HasSession(ctx context.Context) (bool, error)
}

// NotifierPort defines the outbound port for sending real-time operational notifications (Discord, Webhooks).
type NotifierPort interface {
	Notify(ctx context.Context, message string) error
	NotifyPairingCode(ctx context.Context, code string, attempt int) error
	NotifyLogout(ctx context.Context, reason string) error
	NotifyAutoPairingExhausted(ctx context.Context, phone string) error
}

// AntiBanGuardPort defines the outbound port for message rate-limiting, human typing delays, and warmup tiers.
type AntiBanGuardPort interface {
	WaitSendPermit(ctx context.Context, targetJID string) error
	RecordMessageSent(targetJID string)
	GetWarmupStats() map[string]interface{}
	PruneStale(olderThan time.Duration) int
	ResetDaily()
}

// BackupStorePort defines the outbound port for persisting and retrieving chat backup archives.
type BackupStorePort interface {
	SaveBackup(ctx context.Context, result domain.BackupResult, data []byte) error
	ReadBackup(ctx context.Context, backupID string) (domain.BackupResult, []byte, error)
	ListBackups(ctx context.Context) ([]domain.BackupResult, error)
	PurgeOldBackups(ctx context.Context, retention time.Duration) (int, error)
}
