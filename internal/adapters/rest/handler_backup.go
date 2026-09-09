package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

type BackupHandler struct {
	backupService ports.BackupService
}

func NewBackupHandler(backupService ports.BackupService) *BackupHandler {
	return &BackupHandler{backupService: backupService}
}

// CreateBackup archives a group or direct chat with its full history and metadata.
func (h *BackupHandler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	var req domain.BackupRequest

	// Read optional query or path parameters
	if jid := r.PathValue("jid"); jid != "" {
		req.ChatJID = jid
	}

	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	if req.ChatJID == "" {
		req.ChatJID = r.URL.Query().Get("jid")
	}

	if req.ChatJID == "" {
		WriteError(w, http.StatusBadRequest, "chat_jid is required")
		return
	}

	result, err := h.backupService.BackupGroupChat(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusCreated, result)
}

// ListBackups returns all available backup records.
func (h *BackupHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := h.backupService.ListBackups(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, backups)
}

// DownloadBackup streams the raw backup archive file (JSON/text).
func (h *BackupHandler) DownloadBackup(w http.ResponseWriter, r *http.Request) {
	backupID := r.PathValue("id")
	if backupID == "" {
		backupID = r.URL.Query().Get("id")
	}

	if backupID == "" {
		WriteError(w, http.StatusBadRequest, "backup id is required")
		return
	}

	meta, data, err := h.backupService.GetBackup(r.Context(), backupID)
	if err != nil {
		WriteError(w, http.StatusNotFound, "backup not found: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.%s\"", meta.BackupID, meta.Format))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
