package rest

import (
	"encoding/json"
	"net/http"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

type SessionHandler struct {
	sessionService ports.SessionService
}

func NewSessionHandler(sessionService ports.SessionService) *SessionHandler {
	return &SessionHandler{sessionService: sessionService}
}

// Healthz responds to Coolify & Traefik container health monitoring.
func (h *SessionHandler) Healthz(w http.ResponseWriter, r *http.Request) {
	status, err := h.sessionService.GetStatus(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("error: " + err.Error()))
		return
	}

	if status.IsConnected {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}

	// In Coolify, if we return 503 while pairing is in progress, the container might get killed!
	// So if status is StatePairing or StateConnecting, report 200 with notice or 503 if strict.
	// Returning 200 with status payload in JSON or "waiting_pairing" allows the bot to stay alive while user types code.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("waiting_pairing"))
}

// GetStatus returns the current connection state, phone number, and uptime.
func (h *SessionHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.sessionService.GetStatus(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, status)
}

// RequestPairing initiates the phone pairing procedure and generates a 8-digit code.
func (h *SessionHandler) RequestPairing(w http.ResponseWriter, r *http.Request) {
	var req domain.PairingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.PhoneNumber == "" {
		WriteError(w, http.StatusBadRequest, "phone_number is required (format: 6281234567890)")
		return
	}

	code, err := h.sessionService.RequestPairing(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]string{
		"phone_number": req.PhoneNumber,
		"pairing_code": code,
		"message":      "Pairing code generated and dispatched to Discord webhook.",
	})
}

// Disconnect gracefully closes the WhatsApp connection.
func (h *SessionHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.sessionService.Disconnect(r.Context()); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{
		"message": "disconnected successfully",
	})
}
