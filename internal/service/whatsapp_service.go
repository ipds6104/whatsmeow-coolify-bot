package service

import (
	"context"
	"fmt"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

// WhatsAppServiceImpl implements ports.WhatsAppService orchestrating messaging and anti-ban safeguards.
type WhatsAppServiceImpl struct {
	client ports.WhatsAppClientPort
	guard  ports.AntiBanGuardPort
	store  ports.SessionStorePort
}

var _ ports.WhatsAppService = (*WhatsAppServiceImpl)(nil)

func NewWhatsAppService(
	client ports.WhatsAppClientPort,
	guard ports.AntiBanGuardPort,
	store ports.SessionStorePort,
) *WhatsAppServiceImpl {
	return &WhatsAppServiceImpl{
		client: client,
		guard:  guard,
		store:  store,
	}
}

func (w *WhatsAppServiceImpl) SendText(ctx context.Context, msg domain.TextMessage) (string, error) {
	if !w.client.IsConnected() {
		return "", domain.ErrNotConnected
	}
	if !w.client.IsLoggedIn() {
		return "", domain.ErrNotLoggedIn
	}

	if msg.Recipient == "" || msg.Content == "" {
		return "", domain.ErrInvalidRecipient
	}

	// 1. Anti-ban rate limit & human-like delay permit check
	if err := w.guard.WaitSendPermit(ctx, msg.Recipient); err != nil {
		return "", err
	}

	// 2. Transmit via client
	msgID, err := w.client.SendTextMessage(ctx, msg.Recipient, msg.Content, msg.ReplyToID)
	if err != nil {
		return "", fmt.Errorf("failed to dispatch text message: %w", err)
	}

	// 3. Update anti-ban counters
	w.guard.RecordMessageSent(msg.Recipient)

	// 4. Save to durable store for history and backup
	_ = w.store.SaveMessage(ctx, domain.ChatMessage{
		ID:        msgID,
		ChatJID:   msg.Recipient,
		SenderJID: w.client.GetDeviceJID(),
		Timestamp: time.Now(),
		IsFromMe:  true,
		Text:      msg.Content,
	})

	return msgID, nil
}

func (w *WhatsAppServiceImpl) SendMedia(ctx context.Context, msg domain.MediaMessage) (string, error) {
	if !w.client.IsConnected() {
		return "", domain.ErrNotConnected
	}
	if !w.client.IsLoggedIn() {
		return "", domain.ErrNotLoggedIn
	}

	if msg.Recipient == "" || len(msg.Data) == 0 {
		return "", domain.ErrInvalidRecipient
	}

	// Anti-ban throttling
	if err := w.guard.WaitSendPermit(ctx, msg.Recipient); err != nil {
		return "", err
	}

	msgID, err := w.client.SendMediaMessage(ctx, msg.Recipient, msg)
	if err != nil {
		return "", fmt.Errorf("failed to dispatch media message: %w", err)
	}

	w.guard.RecordMessageSent(msg.Recipient)

	_ = w.store.SaveMessage(ctx, domain.ChatMessage{
		ID:        msgID,
		ChatJID:   msg.Recipient,
		SenderJID: w.client.GetDeviceJID(),
		Timestamp: time.Now(),
		IsFromMe:  true,
		Text:      msg.Caption,
		MediaType: string(msg.Type),
		HasMedia:  true,
	})

	return msgID, nil
}

func (w *WhatsAppServiceImpl) ListGroups(ctx context.Context) ([]domain.GroupInfo, error) {
	if !w.client.IsConnected() {
		return nil, domain.ErrNotConnected
	}
	return w.client.GetJoinedGroups(ctx)
}

func (w *WhatsAppServiceImpl) GetGroupInfo(ctx context.Context, groupJID string) (domain.GroupInfo, error) {
	if !w.client.IsConnected() {
		return domain.GroupInfo{}, domain.ErrNotConnected
	}
	return w.client.GetGroupInfo(ctx, groupJID)
}

func (w *WhatsAppServiceImpl) DownloadMedia(ctx context.Context, info domain.MediaDownloadInfo) ([]byte, string, error) {
	if !w.client.IsConnected() {
		return nil, "", domain.ErrNotConnected
	}
	data, err := w.client.DownloadMedia(ctx, info)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", domain.ErrMediaDownloadFailed, err)
	}
	mime := info.MimeType
	if mime == "" {
		mime = "application/octet-stream"
	}
	return data, mime, nil
}

func (w *WhatsAppServiceImpl) GetRecentMessages(ctx context.Context, chatJID string, limit int) ([]domain.ChatMessage, error) {
	return w.store.GetMessages(ctx, chatJID, limit)
}

func (w *WhatsAppServiceImpl) GetAllRecentMessages(ctx context.Context, limit int) ([]domain.ChatMessage, error) {
	return w.store.GetAllMessages(ctx, limit)
}

func (w *WhatsAppServiceImpl) GetAntiBanStats() map[string]interface{} {
	return w.guard.GetWarmupStats()
}
