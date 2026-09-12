package whatsmeow

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/service"
)

type WebhookDeliveryLog struct {
	Timestamp  time.Time `json:"timestamp"`
	MessageID  string    `json:"message_id"`
	ChatJID    string    `json:"chat_jid"`
	SenderJID  string    `json:"sender_jid"`
	Text       string    `json:"text"`
	Status     int       `json:"status"`
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"duration_ms"`
}

type EventTraceLog struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Summary   string    `json:"summary"`
}

type EventHandler struct {
	sessionService *service.SessionServiceImpl
	notifier       ports.NotifierPort
	store          ports.SessionStorePort
	webhookURL     string
	webhookRole    string
	httpClient     *http.Client

	mu               sync.RWMutex
	recentDeliveries []WebhookDeliveryLog
	recentEvents     []EventTraceLog
}

func NewEventHandler(
	sessionService *service.SessionServiceImpl,
	notifier ports.NotifierPort,
	store ports.SessionStorePort,
	webhookURL string,
	webhookRole string,
) *EventHandler {
	if webhookRole == "" {
		webhookRole = "primary_bot"
	}
	return &EventHandler{
		sessionService:   sessionService,
		notifier:         notifier,
		store:            store,
		webhookURL:       webhookURL,
		webhookRole:      webhookRole,
		httpClient:       &http.Client{Timeout: 15 * time.Second},
		recentDeliveries: make([]WebhookDeliveryLog, 0, 30),
		recentEvents:     make([]EventTraceLog, 0, 30),
	}
}

func (h *EventHandler) recordEvent(evtType, summary string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.recentEvents) >= 30 {
		h.recentEvents = h.recentEvents[1:]
	}
	h.recentEvents = append(h.recentEvents, EventTraceLog{
		Timestamp: time.Now(),
		Type:      evtType,
		Summary:   summary,
	})
}

func (h *EventHandler) recordDelivery(dl WebhookDeliveryLog) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.recentDeliveries) >= 30 {
		h.recentDeliveries = h.recentDeliveries[1:]
	}
	h.recentDeliveries = append(h.recentDeliveries, dl)
}

func (h *EventHandler) GetDiagnostics() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	deliveriesCopy := make([]WebhookDeliveryLog, len(h.recentDeliveries))
	copy(deliveriesCopy, h.recentDeliveries)

	eventsCopy := make([]EventTraceLog, len(h.recentEvents))
	copy(eventsCopy, h.recentEvents)

	return map[string]interface{}{
		"webhook_url":       h.webhookURL,
		"webhook_role":      h.webhookRole,
		"recent_deliveries": deliveriesCopy,
		"recent_events":     eventsCopy,
	}
}

func (h *EventHandler) HandleEvent(rawEvt interface{}) {
	ctx := context.Background()

	switch evt := rawEvt.(type) {
	case *events.Connected:
		log.Println("[WHATSMEOW] Terhubung ke WhatsApp server")
		h.sessionService.UpdateLastSeen()
		h.recordEvent("Connected", "Terhubung ke WhatsApp server")
		_ = h.notifier.Notify(ctx, ":white_check_mark: **WhatsApp tersambung.** Layanan REST API siap menerima permintaan.")

	case *events.Disconnected:
		log.Println("[WHATSMEOW] Koneksi WhatsApp terputus sementara (wifi/jaringan). Supervisor akan reconnect.")
		h.recordEvent("Disconnected", "Koneksi WhatsApp terputus sementara")

	case *events.LoggedOut:
		reasonStr := fmt.Sprintf("%v", evt.Reason)
		log.Printf("[WHATSMEOW] LOGOUT PAKSA terdeteksi dari WhatsApp! Alasan: %s", reasonStr)
		h.recordEvent("LoggedOut", reasonStr)
		h.sessionService.TriggerLoggedOut(reasonStr)

	case *events.PairSuccess:
		log.Printf("[WHATSMEOW] Pairing berhasil untuk device %s", evt.ID.String())
		h.sessionService.UpdateLastSeen()
		h.recordEvent("PairSuccess", evt.ID.String())
		_ = h.notifier.Notify(ctx, fmt.Sprintf(":tada: **Pairing berhasil!** Device `%s` aktif kembali.", evt.ID.String()))

	case *events.QR:
		if len(evt.Codes) > 0 {
			log.Printf("[WHATSMEOW] QR Code diterima dari WhatsApp server (panjang string: %d)", len(evt.Codes[0]))
			h.recordEvent("QR", fmt.Sprintf("QR code received len=%d", len(evt.Codes[0])))
			h.sessionService.SetQRCode(evt.Codes[0])
		}

	case *events.Message:
		h.sessionService.UpdateLastSeen()
		h.recordEvent("Message", fmt.Sprintf("ID=%s Chat=%s FromMe=%v", evt.Info.ID, evt.Info.Chat.String(), evt.Info.IsFromMe))
		h.handleIncomingMessage(ctx, evt)

	case *events.Receipt:
		h.sessionService.UpdateLastSeen()
		h.recordEvent("Receipt", fmt.Sprintf("Type=%s Chat=%s", evt.Type, evt.Chat.String()))
	}
}

func (h *EventHandler) handleIncomingMessage(ctx context.Context, evt *events.Message) {
	if evt == nil || evt.Message == nil {
		return
	}

	msgID := evt.Info.ID
	chatJID := evt.Info.Chat.ToNonAD().String()
	senderJID := evt.Info.Sender.ToNonAD().String()
	senderName := evt.Info.PushName
	timestamp := evt.Info.Timestamp
	isFromMe := evt.Info.IsFromMe

	// Handle modern WhatsApp LID resolution for 1-on-1 chats:
	// If the chat or sender is using LID format, and SenderAlt contains a canonical phone number,
	// normalize chatJID and senderJID so downstream systems can identify and route to the phone number.
	var senderAlt string
	if !evt.Info.SenderAlt.IsEmpty() {
		senderAlt = evt.Info.SenderAlt.ToNonAD().String()
	}

	if (evt.Info.Chat.Server == "lid" || evt.Info.Sender.Server == "lid") && senderAlt != "" && !evt.Info.IsGroup {
		log.Printf("[EVENT_HANDLER] Resolving LID addressing to canonical phone JID: %s (LID was %s)", senderAlt, chatJID)
		chatJID = senderAlt
		senderJID = senderAlt
	}

	var text string
	var mediaType string
	var hasMedia bool
	var mediaInfo *domain.MediaDownloadInfo
	var ctxInfo *waE2E.ContextInfo

	if conv := evt.Message.GetConversation(); conv != "" {
		text = conv
	} else if ext := evt.Message.GetExtendedTextMessage(); ext != nil {
		text = ext.GetText()
		ctxInfo = ext.GetContextInfo()
	} else if img := evt.Message.GetImageMessage(); img != nil {
		text = img.GetCaption()
		ctxInfo = img.GetContextInfo()
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
		ctxInfo = vid.GetContextInfo()
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
		ctxInfo = aud.GetContextInfo()
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
		ctxInfo = doc.GetContextInfo()
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
	} else if stk := evt.Message.GetStickerMessage(); stk != nil {
		ctxInfo = stk.GetContextInfo()
		mediaType = "sticker"
		hasMedia = true
	}

	// Extract Quoted Message & Mention metadata from ContextInfo
	var quotedPayload map[string]interface{}
	var mentionedJIDs []string

	if ctxInfo != nil {
		mentionedJIDs = ctxInfo.GetMentionedJID()

		stanzaID := ctxInfo.GetStanzaID()
		participant := ctxInfo.GetParticipant()
		if participant != "" {
			if parsed, err := types.ParseJID(participant); err == nil {
				participant = parsed.ToNonAD().String()
			}
		}

		var quotedText string
		if qMsg := ctxInfo.GetQuotedMessage(); qMsg != nil {
			if qConv := qMsg.GetConversation(); qConv != "" {
				quotedText = qConv
			} else if qExt := qMsg.GetExtendedTextMessage(); qExt != nil {
				quotedText = qExt.GetText()
			} else if qImg := qMsg.GetImageMessage(); qImg != nil {
				quotedText = qImg.GetCaption()
				if quotedText == "" {
					quotedText = "[Foto]"
				}
			} else if qVid := qMsg.GetVideoMessage(); qVid != nil {
				quotedText = qVid.GetCaption()
				if quotedText == "" {
					quotedText = "[Video]"
				}
			} else if qDoc := qMsg.GetDocumentMessage(); qDoc != nil {
				quotedText = qDoc.GetCaption()
				if quotedText == "" {
					quotedText = "[Dokumen]"
				}
			} else if qAud := qMsg.GetAudioMessage(); qAud != nil {
				quotedText = "[Audio/Voice Note]"
			} else if qStk := qMsg.GetStickerMessage(); qStk != nil {
				quotedText = "[Stiker]"
			}
		}

		if stanzaID != "" || quotedText != "" {
			quotedPayload = map[string]interface{}{
				"id":          stanzaID,
				"message_id":  stanzaID,
				"participant": participant,
				"sender":      participant,
				"sender_jid":  participant,
				"text":        quotedText,
				"body":        quotedText,
			}
			log.Printf("[EVENT_HANDLER] Quoted message detected: ID=%s Sender=%s Text=%q", stanzaID, participant, quotedText)
		}
	}

	log.Printf("[EVENT_HANDLER] Incoming message ID=%s Chat=%s Sender=%s IsFromMe=%v Text=%q", msgID, chatJID, senderJID, isFromMe, text)

	var rawData map[string]interface{}
	if quotedPayload != nil || len(mentionedJIDs) > 0 {
		rawData = make(map[string]interface{})
		if quotedPayload != nil {
			rawData["quoted_message"] = quotedPayload
		}
		if len(mentionedJIDs) > 0 {
			rawData["mentioned_jids"] = mentionedJIDs
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
		RawData:    rawData,
	}

	if err := h.store.SaveMessage(ctx, chatMsg); err != nil {
		log.Printf("[EVENT_HANDLER] error saving message %s: %v", msgID, err)
	}

	if h.webhookURL == "" {
		log.Printf("[WEBHOOK] Forwarding skipped for message %s: WEBHOOK_URL is empty", msgID)
		return
	}

	if chatMsg.IsFromMe {
		log.Printf("[WEBHOOK] Forwarding skipped for message %s: message was sent by bot itself (IsFromMe=true)", msgID)
		return
	}

	go h.forwardToWebhook(chatMsg, senderAlt, quotedPayload, mentionedJIDs)
}

func (h *EventHandler) forwardToWebhook(
	msg domain.ChatMessage,
	senderAlt string,
	quotedPayload map[string]interface{},
	mentionedJIDs []string,
) {
	start := time.Now()
	payload := map[string]interface{}{
		"id":           msg.ID,
		"message_id":   msg.ID,
		"chat_jid":     msg.ChatJID,
		"from":         msg.ChatJID,
		"sender_jid":   msg.SenderJID,
		"sender":       msg.SenderJID,
		"sender_name":  msg.SenderName,
		"push_name":    msg.SenderName,
		"text":         msg.Text,
		"body":         msg.Text,
		"timestamp":    msg.Timestamp.Unix(),
		"is_from_me":   msg.IsFromMe,
		"media_type":   msg.MediaType,
		"has_media":    msg.HasMedia,
		"session_role": h.webhookRole,
	}

	if senderAlt != "" {
		payload["sender_alt"] = senderAlt
		payload["phone_number"] = senderAlt
	}

	if quotedPayload != nil {
		payload["quoted_message"] = quotedPayload
		payload["context_info"] = quotedPayload
	}

	if len(mentionedJIDs) > 0 {
		payload["mentioned_jids"] = mentionedJIDs
		payload["mentions"] = mentionedJIDs
	}

	// Extract bot device JID and LID to determine if the bot itself was explicitly mentioned
	var deviceJID, deviceLID string
	if h.sessionService != nil {
		deviceJID = h.sessionService.GetDeviceJID()
		deviceLID = h.sessionService.GetDeviceLID()
	}

	botJIDClean := ""
	if deviceJID != "" {
		botJIDClean = strings.Split(deviceJID, "@")[0]
		botJIDClean = strings.Split(botJIDClean, ":")[0]
	}
	botLIDClean := ""
	if deviceLID != "" {
		botLIDClean = strings.Split(deviceLID, "@")[0]
		botLIDClean = strings.Split(botLIDClean, ":")[0]
	}

	isBotMentioned := false
	for _, m := range mentionedJIDs {
		if (botJIDClean != "" && strings.Contains(m, botJIDClean)) || (botLIDClean != "" && strings.Contains(m, botLIDClean)) {
			isBotMentioned = true
			break
		}
	}
	if botLIDClean != "" && strings.Contains(msg.Text, "@"+botLIDClean) {
		isBotMentioned = true
	}

	if deviceJID != "" {
		payload["bot_jid"] = deviceJID
	}
	if deviceLID != "" {
		payload["bot_lid"] = deviceLID
	}
	payload["is_bot_mentioned"] = isBotMentioned

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[WEBHOOK] JSON serialization failed: %v", err)
		h.recordDelivery(WebhookDeliveryLog{
			Timestamp:  time.Now(),
			MessageID:  msg.ID,
			ChatJID:    msg.ChatJID,
			SenderJID:  msg.SenderJID,
			Text:       msg.Text,
			Error:      err.Error(),
			DurationMs: time.Since(start).Milliseconds(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.webhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[WEBHOOK] Failed constructing HTTP request to %s: %v", h.webhookURL, err)
		h.recordDelivery(WebhookDeliveryLog{
			Timestamp:  time.Now(),
			MessageID:  msg.ID,
			ChatJID:    msg.ChatJID,
			SenderJID:  msg.SenderJID,
			Text:       msg.Text,
			Error:      err.Error(),
			DurationMs: time.Since(start).Milliseconds(),
		})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "whatsmeow-coolify-bot/1.1")

	resp, err := h.httpClient.Do(req)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		log.Printf("[WEBHOOK] Failed forwarding message %s to %s: %v (took %dms)", msg.ID, h.webhookURL, err, durationMs)
		h.recordDelivery(WebhookDeliveryLog{
			Timestamp:  time.Now(),
			MessageID:  msg.ID,
			ChatJID:    msg.ChatJID,
			SenderJID:  msg.SenderJID,
			Text:       msg.Text,
			Error:      err.Error(),
			DurationMs: durationMs,
		})
		return
	}
	defer resp.Body.Close()

	h.recordDelivery(WebhookDeliveryLog{
		Timestamp:  time.Now(),
		MessageID:  msg.ID,
		ChatJID:    msg.ChatJID,
		SenderJID:  msg.SenderJID,
		Text:       msg.Text,
		Status:     resp.StatusCode,
		DurationMs: durationMs,
	})

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("[WEBHOOK] Message %s from %s forwarded successfully to %s (status %d, %dms)", msg.ID, msg.SenderJID, h.webhookURL, resp.StatusCode, durationMs)
	} else {
		log.Printf("[WEBHOOK] Forwarding message %s to %s returned non-2xx status: %d (%dms)", msg.ID, h.webhookURL, resp.StatusCode, durationMs)
	}
}
