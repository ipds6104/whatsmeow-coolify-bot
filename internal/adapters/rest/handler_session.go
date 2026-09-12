package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

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

// GetQRCode provides live QR Code pairing data or a standalone HTML scanner UI.
func (h *SessionHandler) GetQRCode(w http.ResponseWriter, r *http.Request) {
	qr, err := h.sessionService.GetQRCode(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	accept := r.Header.Get("Accept")
	format := r.URL.Query().Get("format")
	if format != "json" && strings.Contains(accept, "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(renderQRHTML(qr)))
		return
	}

	WriteJSON(w, http.StatusOK, qr)
}

func renderQRHTML(qr domain.QRCodeResult) string {
	if qr.IsLoggedIn {
		return `<!DOCTYPE html>
<html lang="id">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>WhatsApp Bot - Connected</title>
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #111b21; color: #e9edef; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; }
		.card { background: #202c33; padding: 32px; border-radius: 12px; text-align: center; box-shadow: 0 4px 12px rgba(0,0,0,0.3); max-width: 400px; }
		.badge { background: #00a884; color: white; padding: 8px 16px; border-radius: 20px; font-weight: bold; display: inline-block; margin-bottom: 16px; }
		h1 { font-size: 20px; margin: 0 0 8px 0; }
		p { color: #8696a0; font-size: 14px; margin: 0 0 20px 0; }
		a { color: #53bdeb; text-decoration: none; font-size: 14px; }
	</style>
</head>
<body>
	<div class="card">
		<div class="badge">✓ TERHUBUNG</div>
		<h1>WhatsApp Aktif Permanen</h1>
		<p>Sesi autentikasi tersimpan aman di database PostgreSQL. Anda tidak perlu memindai QR code lagi.</p>
		<a href="/api/v1/session/status">Lihat Diagnostik Status API &rarr;</a>
	</div>
</body>
</html>`
	}

	qrURL := ""
	if qr.QRCode != "" {
		qrURL = fmt.Sprintf("https://api.qrserver.com/v1/create-qr-code/?size=280x280&data=%s", url.QueryEscape(qr.QRCode))
	}

	timerText := fmt.Sprintf("Rotasi QR berikutnya dalam: <b>%d detik</b>", qr.ExpiresInSec)
	if qr.ExpiresInSec <= 0 {
		timerText = `<span style="color: #f15c6d;">Menghubungkan ulang ke server WhatsApp untuk memuat QR baru...</span>`
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="id">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>WhatsApp Bot - Scan QR / Pairing Code</title>
	<meta http-equiv="refresh" content="3">
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #111b21; color: #e9edef; display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; padding: 20px; box-sizing: border-box; }
		.card { background: #202c33; padding: 32px; border-radius: 16px; text-align: center; box-shadow: 0 8px 24px rgba(0,0,0,0.4); max-width: 440px; width: 100%%; }
		.header { font-size: 22px; font-weight: bold; margin-bottom: 8px; color: #25D366; }
		.sub { color: #8696a0; font-size: 14px; margin-bottom: 24px; line-height: 1.5; }
		.qr-box { background: white; padding: 16px; border-radius: 12px; display: inline-block; margin-bottom: 20px; }
		.qr-box img { display: block; width: 240px; height: 240px; }
		.pairing-box { background: #111b21; border: 1px solid #2a3942; border-radius: 8px; padding: 12px; margin-top: 16px; }
		.code { font-family: monospace; font-size: 24px; font-weight: bold; letter-spacing: 4px; color: #25D366; }
		.steps { text-align: left; background: #182229; padding: 16px; border-radius: 8px; margin-top: 20px; font-size: 13px; color: #d1d7db; line-height: 1.6; }
		.steps ol { margin: 0; padding-left: 20px; }
		.timer { font-size: 12px; color: #8696a0; margin-top: 12px; }
	</style>
</head>
<body>
	<div class="card">
		<div class="header">Tautkan WhatsApp Bot</div>
		<div class="sub">Pindai QR Code di bawah atau gunakan kode pairing untuk menghubungkan nomor Anda. Halaman ini auto-refresh tiap 3 detik.</div>
		<div class="qr-box">
			<img src="%s" alt="WhatsApp QR Code" />
		</div>
		<div class="timer">%s</div>
		<div class="pairing-box">
			<div style="font-size: 12px; color: #8696a0; margin-bottom: 4px;">Atau Kode Pairing:</div>
			<div class="code">%s</div>
		</div>
		<div class="steps">
			<b>Cara Menghubungkan di HP:</b>
			<ol>
				<li>Buka WhatsApp di ponsel Anda</li>
				<li>Pilih <b>Setelan / Titik Tiga</b> ➔ <b>Perangkat Tertaut</b></li>
				<li>Pilih <b>Tautkan Perangkat</b></li>
				<li>Arahkan kamera ke QR Code di atas, atau pilih <i>"Tautkan dengan nomor telepon saja"</i> lalu masukkan kode di atas</li>
			</ol>
		</div>
	</div>
</body>
</html>`, qrURL, timerText, qr.PairingCode)
}
