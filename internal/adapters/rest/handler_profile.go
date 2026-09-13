package rest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

type ProfileHandler struct {
	waService ports.WhatsAppService
}

func NewProfileHandler(waService ports.WhatsAppService) *ProfileHandler {
	return &ProfileHandler{waService: waService}
}

// GetProfilePicture fetches the profile picture URL for a user, group, or self.
// Query params:
// - jid: WhatsApp JID (e.g. 62812345678@s.whatsapp.net, xxx@g.us, or empty/me for self)
// - preview: bool (true for thumbnail preview, false for full image)
func (h *ProfileHandler) GetProfilePicture(w http.ResponseWriter, r *http.Request) {
	jid := r.URL.Query().Get("jid")
	previewStr := r.URL.Query().Get("preview")
	preview, _ := strconv.ParseBool(previewStr)

	res, err := h.waService.GetProfilePicture(r.Context(), jid, preview)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    res,
	})
}

// SetProfilePicture updates the profile picture for the bot itself or a group.
// Supports multipart/form-data or JSON (base64 image).
func (h *ProfileHandler) SetProfilePicture(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	var jid string
	var avatarBytes []byte

	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			WriteError(w, http.StatusBadRequest, "failed to parse multipart form: "+err.Error())
			return
		}
		jid = r.FormValue("jid")
		file, _, err := r.FormFile("file")
		if err != nil {
			file, _, err = r.FormFile("avatar")
		}
		if err != nil {
			WriteError(w, http.StatusBadRequest, "file or avatar field is required in multipart form")
			return
		}
		defer file.Close()

		var readErr error
		avatarBytes, readErr = io.ReadAll(file)
		if readErr != nil {
			WriteError(w, http.StatusBadRequest, "failed to read uploaded avatar: "+readErr.Error())
			return
		}
	} else {
		var req struct {
			JID         string `json:"jid"`
			DataBase64 string `json:"data_base64"`
			ImageBase64 string `json:"image_base64"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error())
			return
		}
		jid = req.JID
		b64 := req.DataBase64
		if b64 == "" {
			b64 = req.ImageBase64
		}
		if b64 == "" {
			WriteError(w, http.StatusBadRequest, "data_base64 or image_base64 is required")
			return
		}

		// Strip data URI scheme prefix if provided (e.g. data:image/jpeg;base64,...)
		if idx := strings.Index(b64, ","); idx != -1 {
			b64 = b64[idx+1:]
		}

		var decErr error
		avatarBytes, decErr = base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if decErr != nil {
			WriteError(w, http.StatusBadRequest, "invalid base64 encoding: "+decErr.Error())
			return
		}
	}

	if len(avatarBytes) == 0 {
		WriteError(w, http.StatusBadRequest, "avatar data cannot be empty")
		return
	}

	pictureID, err := h.waService.SetProfilePicture(r.Context(), jid, avatarBytes)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to set profile picture: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"picture_id": pictureID,
		"jid":        jid,
		"message":    "Profile picture updated successfully",
	})
}

// RemoveProfilePicture removes the profile picture of the bot or a group.
func (h *ProfileHandler) RemoveProfilePicture(w http.ResponseWriter, r *http.Request) {
	jid := r.URL.Query().Get("jid")
	if jid == "" && r.Body != nil {
		var req struct {
			JID string `json:"jid"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		jid = req.JID
	}

	_, err := h.waService.SetProfilePicture(r.Context(), jid, nil)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to remove profile picture: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"jid":     jid,
		"message": "Profile picture removed successfully",
	})
}

// SetAboutStatus updates the bot's "About" bio text.
func (h *ProfileHandler) SetAboutStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error())
		return
	}

	statusText := req.Status
	if statusText == "" {
		statusText = req.Text
	}
	if statusText == "" {
		WriteError(w, http.StatusBadRequest, "status or text field is required")
		return
	}

	if err := h.waService.SetStatusMessage(r.Context(), statusText); err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to update about status: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"status":  statusText,
		"message": "About status updated successfully",
	})
}

// SendStatusStory sends an ephemeral 24-hour status story (text or media) to WhatsApp status broadcast.
func (h *ProfileHandler) SendStatusStory(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	var msg domain.StatusBroadcastMessage

	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(50 << 20); err != nil {
			WriteError(w, http.StatusBadRequest, "failed to parse multipart: "+err.Error())
			return
		}
		msg.Text = r.FormValue("text")
		msg.Caption = r.FormValue("caption")
		msg.Type = domain.MediaType(r.FormValue("type"))
		if bgStr := r.FormValue("background_color"); bgStr != "" {
			if parsed, err := strconv.ParseUint(strings.TrimPrefix(bgStr, "0x"), 16, 32); err == nil {
				msg.BackgroundColor = uint32(parsed)
			}
		}
		if fontStr := r.FormValue("font"); fontStr != "" {
			if parsed, err := strconv.ParseInt(fontStr, 10, 32); err == nil {
				msg.Font = int32(parsed)
			}
		}

		file, header, err := r.FormFile("file")
		if err == nil {
			defer file.Close()
			data, readErr := io.ReadAll(file)
			if readErr != nil {
				WriteError(w, http.StatusBadRequest, "failed to read file: "+readErr.Error())
				return
			}
			msg.Data = data
			msg.FileName = header.Filename
			msg.MimeType = header.Header.Get("Content-Type")
			if msg.Type == "" {
				if strings.HasPrefix(msg.MimeType, "video/") {
					msg.Type = domain.MediaTypeVideo
				} else {
					msg.Type = domain.MediaTypeImage
				}
			}
		}
	} else {
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		if msg.DataB64 != "" {
			b64 := msg.DataB64
			if idx := strings.Index(b64, ","); idx != -1 {
				b64 = b64[idx+1:]
			}
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
			if err != nil {
				WriteError(w, http.StatusBadRequest, "invalid base64 media data: "+err.Error())
				return
			}
			msg.Data = decoded
		}
	}

	statusID, err := h.waService.SendStatusBroadcast(r.Context(), msg)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to post status story: "+err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"status_id": statusID,
		"message":   "Status story broadcast posted successfully",
	})
}

// RevokeStatusOrMessage deletes/revokes a sent message or status story for everyone.
func (h *ProfileHandler) RevokeStatusOrMessage(w http.ResponseWriter, r *http.Request) {
	var chatJID, messageID string

	// Check path value if called via /api/v1/status/{id}
	if pathID := r.PathValue("id"); pathID != "" {
		messageID = pathID
		chatJID = "status@broadcast"
	}

	if messageID == "" && r.Body != nil {
		var req struct {
			ChatJID   string `json:"chat_jid"`
			MessageID string `json:"message_id"`
			ID        string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			chatJID = req.ChatJID
			messageID = req.MessageID
			if messageID == "" {
				messageID = req.ID
			}
		}
	}

	if chatJID == "" {
		chatJID = "status@broadcast"
	}

	if messageID == "" {
		WriteError(w, http.StatusBadRequest, "message_id is required")
		return
	}

	if err := h.waService.RevokeMessage(r.Context(), chatJID, messageID); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("failed to revoke message %s: %v", messageID, err))
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"message_id": messageID,
		"chat_jid":   chatJID,
		"message":    "Message or status revoked successfully",
	})
}

// ListStatusStories lists posted status stories (from self or contacts).
// Query params:
// - limit: int (default 50)
// - self: bool (default true) - if true, only list statuses posted by this bot
// - active_only: bool (default false) - if true, only list statuses < 24h old
func (h *ProfileHandler) ListStatusStories(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	selfOnly := true
	if selfStr := r.URL.Query().Get("self"); selfStr != "" {
		if parsed, err := strconv.ParseBool(selfStr); err == nil {
			selfOnly = parsed
		}
	}

	activeOnly := false
	if actStr := r.URL.Query().Get("active_only"); actStr != "" {
		if parsed, err := strconv.ParseBool(actStr); err == nil {
			activeOnly = parsed
		}
	}

	messages, err := h.waService.GetRecentMessages(r.Context(), "status@broadcast", limit*2)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to get status stories: "+err.Error())
		return
	}

	now := time.Now()
	var stories []map[string]interface{}
	for _, m := range messages {
		if selfOnly && !m.IsFromMe {
			continue
		}
		age := now.Sub(m.Timestamp)
		isExpired := age > 24*time.Hour
		if activeOnly && isExpired {
			continue
		}

		expiresInSec := int64((24*time.Hour - age) / time.Second)
		if expiresInSec < 0 {
			expiresInSec = 0
		}

		stories = append(stories, map[string]interface{}{
			"id":                 m.ID,
			"sender_jid":         m.SenderJID,
			"sender_name":        m.SenderName,
			"timestamp":          m.Timestamp.Format(time.RFC3339),
			"is_from_me":         m.IsFromMe,
			"text":               m.Text,
			"media_type":         m.MediaType,
			"has_media":          m.HasMedia,
			"is_expired":         isExpired,
			"age_hours":          fmt.Sprintf("%.1f", age.Hours()),
			"expires_in_seconds": expiresInSec,
		})
		if len(stories) >= limit {
			break
		}
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"count":   len(stories),
		"stories": stories,
	})
}
