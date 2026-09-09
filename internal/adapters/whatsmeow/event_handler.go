package whatsmeow

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"

	"go.mau.fi/whatsmeow/types/events"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/service"
)

type EventHandler struct {
	sessionService *service.SessionServiceImpl
	notifier       ports.NotifierPort
	store          ports.SessionStorePort
}

func NewEventHandler(
	sessionService *service.SessionServiceImpl,
	notifier ports.NotifierPort,
	store ports.SessionStorePort,
) *EventHandler {
	return &EventHandler{
		sessionService: sessionService,
		notifier:       notifier,
		store:          store,
	}
}

func (h *EventHandler) HandleEvent(rawEvt interface{}) {
	ctx := context.Background()

	switch evt := rawEvt.(type) {
	case *events.Connected:
		log.Println("[WHATSMEOW] Terhubung ke WhatsApp server")
		h.sessionService.UpdateLastSeen()
		_ = h.notifier.Notify(ctx, ":white_check_mark: **WhatsApp tersambung.** Layanan REST API siap menerima permintaan.")

	case *events.Disconnected:
		log.Println("[WHATSMEOW] Koneksi WhatsApp terputus sementara (wifi/jaringan). Supervisor akan reconnect.")

	case *events.LoggedOut:
		reasonStr := fmt.Sprintf("%v", evt.Reason)
		log.Printf("[WHATSMEOW] LOGOUT PAKSA terdeteksi dari WhatsApp! Alasan: %s", reasonStr)
		h.sessionService.TriggerLoggedOut(reasonStr)

	case *events.PairSuccess:
		log.Printf("[WHATSMEOW] Pairing berhasil untuk device %s", evt.ID.String())
		h.sessionService.UpdateLastSeen()
		_ = h.notifier.Notify(ctx, fmt.Sprintf(":tada: **Pairing berhasil!** Device `%s` aktif kembali.", evt.ID.String()))

	case *events.Message:
		h.sessionService.UpdateLastSeen()
		h.handleIncomingMessage(ctx, evt)
	}
}

func (h *EventHandler) handleIncomingMessage(ctx context.Context, evt *events.Message) {
	if evt == nil || evt.Message == nil {
		return
	}

	msgID := evt.Info.ID
	chatJID := evt.Info.Chat.String()
	senderJID := evt.Info.Sender.String()
	senderName := evt.Info.PushName
	timestamp := evt.Info.Timestamp
	isFromMe := evt.Info.IsFromMe

	var text string
	var mediaType string
	var hasMedia bool
	var mediaInfo *domain.MediaDownloadInfo

	if conv := evt.Message.GetConversation(); conv != "" {
		text = conv
	} else if ext := evt.Message.GetExtendedTextMessage(); ext != nil {
		text = ext.GetText()
	} else if img := evt.Message.GetImageMessage(); img != nil {
		text = img.GetCaption()
		mediaType = string(domain.MediaTypeImage)
		hasMedia = true
		mediaInfo = &domain.MediaDownloadInfo{
			DirectPath:  img.GetDirectPath(),
			EncFileHash: hex.EncodeToString(img.GetFileEncSHA256()),
			FileSha256:  hex.EncodeToString(img.GetFileSHA256()),
			MediaKey:    hex.EncodeToString(img.GetMediaKey()),
			FileLength:  img.GetFileLength(),
			MimeType:    img.GetMimetype(),
			MediaType:   mediaType,
		}
	} else if vid := evt.Message.GetVideoMessage(); vid != nil {
		text = vid.GetCaption()
		mediaType = string(domain.MediaTypeVideo)
		hasMedia = true
		mediaInfo = &domain.MediaDownloadInfo{
			DirectPath:  vid.GetDirectPath(),
			EncFileHash: hex.EncodeToString(vid.GetFileEncSHA256()),
			FileSha256:  hex.EncodeToString(vid.GetFileSHA256()),
			MediaKey:    hex.EncodeToString(vid.GetMediaKey()),
			FileLength:  vid.GetFileLength(),
			MimeType:    vid.GetMimetype(),
			MediaType:   mediaType,
		}
	} else if aud := evt.Message.GetAudioMessage(); aud != nil {
		mediaType = string(domain.MediaTypeAudio)
		hasMedia = true
		mediaInfo = &domain.MediaDownloadInfo{
			DirectPath:  aud.GetDirectPath(),
			EncFileHash: hex.EncodeToString(aud.GetFileEncSHA256()),
			FileSha256:  hex.EncodeToString(aud.GetFileSHA256()),
			MediaKey:    hex.EncodeToString(aud.GetMediaKey()),
			FileLength:  aud.GetFileLength(),
			MimeType:    aud.GetMimetype(),
			MediaType:   mediaType,
		}
	} else if doc := evt.Message.GetDocumentMessage(); doc != nil {
		text = doc.GetCaption()
		mediaType = string(domain.MediaTypeDocument)
		hasMedia = true
		mediaInfo = &domain.MediaDownloadInfo{
			DirectPath:  doc.GetDirectPath(),
			EncFileHash: hex.EncodeToString(doc.GetFileEncSHA256()),
			FileSha256:  hex.EncodeToString(doc.GetFileSHA256()),
			MediaKey:    hex.EncodeToString(doc.GetMediaKey()),
			FileLength:  doc.GetFileLength(),
			MimeType:    doc.GetMimetype(),
			MediaType:   mediaType,
		}
	}

	chatMsg := domain.ChatMessage{
		ID:         msgID,
		ChatJID:    chatJID,
		SenderJID:  senderJID,
		SenderName: senderName,
		Timestamp:  timestamp,
		IsFromMe:   isFromMe,
		Text:       text,
		MediaType:  mediaType,
		HasMedia:   hasMedia,
		MediaInfo:  mediaInfo,
	}

	if err := h.store.SaveMessage(ctx, chatMsg); err != nil {
		log.Printf("[EVENT_HANDLER] error saving message %s: %v", msgID, err)
	}
}
