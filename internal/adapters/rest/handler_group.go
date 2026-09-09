package rest

import (
	"net/http"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

type GroupHandler struct {
	waService ports.WhatsAppService
}

func NewGroupHandler(waService ports.WhatsAppService) *GroupHandler {
	return &GroupHandler{waService: waService}
}

// ListGroups returns all groups the connected WhatsApp account is currently participating in.
func (h *GroupHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.waService.ListGroups(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, groups)
}

// GetGroupInfo returns rich metadata and participant list for a specific group.
func (h *GroupHandler) GetGroupInfo(w http.ResponseWriter, r *http.Request) {
	groupJID := r.PathValue("jid")
	if groupJID == "" {
		groupJID = r.URL.Query().Get("jid")
	}

	if groupJID == "" {
		WriteError(w, http.StatusBadRequest, "group jid is required")
		return
	}

	info, err := h.waService.GetGroupInfo(r.Context(), groupJID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, info)
}
