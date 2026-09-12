package rest

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

type MessageHandler struct {
	waService ports.WhatsAppService
}

func NewMessageHandler(waService ports.WhatsAppService) *MessageHandler {
	return &MessageHandler{waService: waService}
}

// SendText dispatches an anti-ban protected text message.
func (h *MessageHandler) SendText(w http.ResponseWriter, r *http.Request) {
	var msg domain.TextMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	msgID, err := h.waService.SendText(r.Context(), msg)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message_id": msgID,
		"recipient":  msg.Recipient,
		"status":     "sent",
	})
}

// SendMedia handles sending images, documents, audio, or video files (via Base64 or multipart).
func (h *MessageHandler) SendMedia(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")

	var msg domain.MediaMessage

	if strings.HasPrefix(contentType, "multipart/form-data") {
		err := r.ParseMultipartForm(50 << 20) // 50MB max
		if err != nil {
			WriteError(w, http.StatusBadRequest, "failed to parse multipart: "+err.Error())
			return
		}

		msg.Recipient = r.FormValue("recipient")
		msg.Caption = r.FormValue("caption")
		msg.Type = domain.MediaType(r.FormValue("type"))
		msg.ReplyToID = r.FormValue("reply_to_id")

		file, header, err := r.FormFile("file")
		if err != nil {
			WriteError(w, http.StatusBadRequest, "file parameter is required: "+err.Error())
			return
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "failed to read file: "+err.Error())
			return
		}

		msg.Data = data
		msg.FileName = header.Filename
		msg.MimeType = header.Header.Get("Content-Type")
	} else {
		// JSON with base64 data
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		if msg.DataB64 != "" {
			decoded, err := base64.StdEncoding.DecodeString(msg.DataB64)
			if err != nil {
				WriteError(w, http.StatusBadRequest, "invalid base64 in data_base64: "+err.Error())
				return
			}
			msg.Data = decoded
		}
	}

	if len(msg.Data) == 0 {
		WriteError(w, http.StatusBadRequest, "file data cannot be empty")
		return
	}

	msgID, err := h.waService.SendMedia(r.Context(), msg)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message_id": msgID,
		"recipient":  msg.Recipient,
		"type":       msg.Type,
		"status":     "sent",
	})
}

// DownloadMedia downloads and decrypts a media file from WhatsApp CDN using media keys.
func (h *MessageHandler) DownloadMedia(w http.ResponseWriter, r *http.Request) {
	var info domain.MediaDownloadInfo
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid download info: "+err.Error())
		return
	}

	data, mimeType, err := h.waService.DownloadMedia(r.Context(), info)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// GetChatMessages retrieves stored chat history for a given JID.
func (h *MessageHandler) GetChatMessages(w http.ResponseWriter, r *http.Request) {
	// Extract chatJID from path (using r.PathValue in Go 1.22+)
	chatJID := r.PathValue("jid")
	if chatJID == "" {
		chatJID = r.URL.Query().Get("jid")
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	messages, err := h.waService.GetRecentMessages(r.Context(), chatJID, limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, messages)
}

// GetAllRecentMessages retrieves recent messages across all conversations.
func (h *MessageHandler) GetAllRecentMessages(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	messages, err := h.waService.GetAllRecentMessages(r.Context(), limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, messages)
}

// GetAntiBanStats exposes live diagnostic metrics from the anti-ban guard.
func (h *MessageHandler) GetAntiBanStats(w http.ResponseWriter, r *http.Request) {
	stats := h.waService.GetAntiBanStats()
	WriteJSON(w, http.StatusOK, stats)
}
