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
	apiKey         string
}

func NewSessionHandler(sessionService ports.SessionService, apiKey string) *SessionHandler {
	return &SessionHandler{sessionService: sessionService, apiKey: apiKey}
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

// WebPairingPage renders the interactive Web Pairing Dashboard.
func (h *SessionHandler) WebPairingPage(w http.ResponseWriter, r *http.Request) {
	status, _ := h.sessionService.GetStatus(r.Context())
	qr, _ := h.sessionService.GetQRCode(r.Context())

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(renderWebPairingDashboard(status, qr, h.apiKey)))
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
		status, _ := h.sessionService.GetStatus(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(renderWebPairingDashboard(status, qr, h.apiKey)))
		return
	}

	WriteJSON(w, http.StatusOK, qr)
}

func renderWebPairingDashboard(status domain.SessionStatus, qr domain.QRCodeResult, defaultAPIKey string) string {
	qrURL := ""
	if qr.QRCode != "" {
		qrURL = fmt.Sprintf("https://api.qrserver.com/v1/create-qr-code/?size=260x260&data=%s", url.QueryEscape(qr.QRCode))
	}

	isLoggedIn := status.IsLoggedIn || qr.IsLoggedIn
	initialCode := qr.PairingCode

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="id">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>WhatsApp Bot - Device Pairing Dashboard</title>
	<style>
		:root {
			--bg-primary: #111b21;
			--bg-secondary: #202c33;
			--bg-tertiary: #182229;
			--accent: #00a884;
			--accent-hover: #02906f;
			--text-primary: #e9edef;
			--text-secondary: #8696a0;
			--border: #2a3942;
			--danger: #ef4444;
			--warning: #f59e0b;
		}
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
			background-color: var(--bg-primary);
			color: var(--text-primary);
			min-height: 100vh;
			display: flex;
			justify-content: center;
			align-items: center;
			padding: 20px;
		}
		.container {
			background-color: var(--bg-secondary);
			border: 1px solid var(--border);
			border-radius: 16px;
			max-width: 480px;
			width: 100%%;
			padding: 28px;
			box-shadow: 0 12px 32px rgba(0,0,0,0.5);
			text-align: center;
		}
		.logo-badge {
			display: inline-flex;
			align-items: center;
			gap: 8px;
			padding: 6px 14px;
			border-radius: 20px;
			font-size: 12px;
			font-weight: 700;
			letter-spacing: 0.5px;
			text-transform: uppercase;
			margin-bottom: 16px;
		}
		.badge-connected { background: rgba(0, 168, 132, 0.15); color: var(--accent); border: 1px solid var(--accent); }
		.badge-waiting { background: rgba(245, 158, 11, 0.15); color: var(--warning); border: 1px solid var(--warning); }
		h1 { font-size: 22px; font-weight: 700; margin-bottom: 8px; }
		p.sub { font-size: 14px; color: var(--text-secondary); line-height: 1.5; margin-bottom: 24px; }
		
		.tabs { display: flex; border-bottom: 1px solid var(--border); margin-bottom: 20px; gap: 8px; }
		.tab-btn {
			flex: 1;
			background: none;
			border: none;
			padding: 10px;
			color: var(--text-secondary);
			font-size: 14px;
			font-weight: 600;
			cursor: pointer;
			border-bottom: 2px solid transparent;
			transition: all 0.2s;
		}
		.tab-btn.active { color: var(--accent); border-bottom-color: var(--accent); }
		.tab-content { display: none; }
		.tab-content.active { display: block; }
		
		.card-box {
			background-color: var(--bg-tertiary);
			border: 1px solid var(--border);
			border-radius: 12px;
			padding: 20px;
			margin-bottom: 20px;
		}
		.code-display {
			font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
			font-size: 28px;
			font-weight: 800;
			letter-spacing: 6px;
			color: var(--accent);
			background: rgba(0, 168, 132, 0.08);
			padding: 14px;
			border-radius: 8px;
			margin: 12px 0;
			border: 1px dashed var(--accent);
			user-select: all;
		}
		.qr-wrapper {
			background: white;
			display: inline-block;
			padding: 12px;
			border-radius: 12px;
			margin: 12px 0;
		}
		.qr-wrapper img { display: block; width: 220px; height: 220px; }
		
		.btn {
			display: inline-flex;
			align-items: center;
			justify-content: center;
			gap: 8px;
			width: 100%%;
			padding: 12px 18px;
			border-radius: 8px;
			font-size: 14px;
			font-weight: 600;
			cursor: pointer;
			transition: all 0.2s;
			border: none;
		}
		.btn-primary { background-color: var(--accent); color: white; }
		.btn-primary:hover { background-color: var(--accent-hover); }
		.btn-secondary { background-color: transparent; border: 1px solid var(--border); color: var(--text-primary); margin-top: 10px; }
		.btn-secondary:hover { background-color: rgba(255,255,255,0.05); }
		.btn-danger { background-color: rgba(239, 68, 68, 0.15); color: var(--danger); border: 1px solid var(--danger); margin-top: 12px; }
		.btn-danger:hover { background-color: var(--danger); color: white; }
		
		.input-group { display: flex; gap: 8px; margin-top: 12px; }
		.input-text {
			flex: 1;
			background-color: var(--bg-primary);
			border: 1px solid var(--border);
			border-radius: 8px;
			padding: 10px 14px;
			color: var(--text-primary);
			font-size: 14px;
			outline: none;
		}
		.input-text:focus { border-color: var(--accent); }
		
		.guide { text-align: left; font-size: 13px; color: #d1d7db; line-height: 1.6; }
		.guide ol { padding-left: 18px; margin-top: 8px; }
		.guide li { margin-bottom: 6px; }
		
		.stat-row { display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid rgba(255,255,255,0.05); font-size: 13px; }
		.stat-label { color: var(--text-secondary); }
		.stat-value { font-weight: 600; color: var(--text-primary); }
		
		.toast {
			position: fixed;
			bottom: 20px;
			left: 50%%;
			transform: translateX(-50%%);
			background: #323739;
			color: white;
			padding: 10px 20px;
			border-radius: 8px;
			font-size: 13px;
			display: none;
			box-shadow: 0 4px 12px rgba(0,0,0,0.4);
			z-index: 100;
		}
	</style>
</head>
<body>
	<div class="container">
		<div id="connected-view" style="display: %s;">
			<div class="logo-badge badge-connected">● TERHUBUNG AKTIF</div>
			<h1>WhatsApp Bot Siap</h1>
			<p class="sub">Daemon WhatsApp berjalan normal dan terhubung ke server. Sesi tersimpan aman di database PostgreSQL.</p>
			
			<div class="card-box">
				<div class="stat-row">
					<span class="stat-label">Nomor WhatsApp</span>
					<span class="stat-value" id="disp-phone">%s</span>
				</div>
				<div class="stat-row">
					<span class="stat-label">Platform</span>
					<span class="stat-value">%s</span>
				</div>
				<div class="stat-row">
					<span class="stat-label">Device JID</span>
					<span class="stat-value" id="disp-jid" style="font-family: monospace; font-size: 11px;">%s</span>
				</div>
				<div class="stat-row">
					<span class="stat-label">Uptime</span>
					<span class="stat-value">%s</span>
				</div>
			</div>
			
			<a href="/api/v1/session/status" class="btn btn-secondary">🔍 Lihat Data JSON Status</a>
			<button type="button" onclick="disconnectSession()" class="btn btn-danger">⚠️ Putuskan Sesi (Disconnect)</button>
		</div>

		<div id="waiting-view" style="display: %s;">
			<div class="logo-badge badge-waiting">● PERLU PENAUTAN</div>
			<h1>Tautkan WhatsApp</h1>
			<p class="sub">Pilih metode penautan perangkat di bawah untuk menghubungkan WhatsApp ke bot.</p>
			
			<div class="tabs">
				<button type="button" class="tab-btn active" onclick="switchTab('tab-code')">🔑 Kode Pairing</button>
				<button type="button" class="tab-btn" onclick="switchTab('tab-qr')">📷 Scan QR Code</button>
			</div>

			<!-- TAB 1: KODE PAIRING -->
			<div id="tab-code" class="tab-content active">
				<div class="card-box">
					<div style="font-size: 12px; color: var(--text-secondary); text-transform: uppercase; font-weight: 600;">Kode Pairing 8-Digit:</div>
					<div class="code-display" id="pairing-code-box">%s</div>
					<button type="button" class="btn btn-primary" onclick="copyPairingCode()">📋 Salin Kode Pairing</button>
				</div>
				
				<div class="card-box" style="margin-top: -8px;">
					<div style="font-size: 13px; font-weight: 600; margin-bottom: 6px; text-align: left;">Minta Kode Pairing Baru:</div>
					<div class="input-group">
						<input type="text" id="phone-input" class="input-text" placeholder="6281234567890" value="%s" />
						<button type="button" class="btn btn-primary" style="width: auto; padding: 0 16px;" onclick="requestNewPairing()">Minta Kode</button>
					</div>
				</div>

				<div class="guide card-box">
					<b>📲 Langkah di WhatsApp HP:</b>
					<ol>
						<li>Buka WhatsApp di HP Anda</li>
						<li>Pilih <b>Setelan</b> ➔ <b>Perangkat Tertaut</b></li>
						<li>Pilih <b>Tautkan Perangkat</b></li>
						<li>Pilih <i>"Tautkan dengan nomor telepon saja"</i> di bagian bawah</li>
						<li>Masukkan kode 8-digit di atas</li>
					</ol>
				</div>
			</div>

			<!-- TAB 2: QR CODE SCANNER -->
			<div id="tab-qr" class="tab-content">
				<div class="card-box">
					<div class="qr-wrapper">
						<img id="qr-image" src="%s" alt="WhatsApp QR Code" />
					</div>
					<div style="font-size: 12px; color: var(--text-secondary); margin-top: 8px;">Arahkan kamera scanner WhatsApp ke QR Code di atas.</div>
				</div>
				
				<div class="guide card-box">
					<b>📷 Cara Scan QR:</b>
					<ol>
						<li>Buka WhatsApp di HP ➔ <b>Perangkat Tertaut</b></li>
						<li>Tekan tombol <b>Tautkan Perangkat</b></li>
						<li>Arahkan kamera ke QR Code di atas</li>
					</ol>
				</div>
			</div>

			<div style="margin-top: 16px;">
				<input type="password" id="api-key-input" class="input-text" placeholder="Masukkan API Key (jika diperlukan)" value="%s" style="margin-bottom: 8px; text-align: center;" />
			</div>
		</div>
	</div>

	<div id="toast" class="toast">Kode berhasil disalin!</div>

	<script>
		let activeAPIKey = new URLSearchParams(window.location.search).get('key') 
			|| new URLSearchParams(window.location.search).get('api_key') 
			|| '%s'
			|| localStorage.getItem('wa_api_key') 
			|| '';

		if (activeAPIKey) {
			const keyInput = document.getElementById('api-key-input');
			if (keyInput) keyInput.value = activeAPIKey;
			localStorage.setItem('wa_api_key', activeAPIKey);
		}

		document.getElementById('api-key-input')?.addEventListener('change', function(e) {
			activeAPIKey = e.target.value.trim();
			localStorage.setItem('wa_api_key', activeAPIKey);
		});

		function switchTab(tabId) {
			document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
			document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
			
			if (tabId === 'tab-code') {
				document.querySelectorAll('.tab-btn')[0].classList.add('active');
			} else {
				document.querySelectorAll('.tab-btn')[1].classList.add('active');
			}
			document.getElementById(tabId).classList.add('active');
		}

		function showToast(msg) {
			const t = document.getElementById('toast');
			t.innerText = msg;
			t.style.display = 'block';
			setTimeout(() => { t.style.display = 'none'; }, 2500);
		}

		function copyPairingCode() {
			const code = document.getElementById('pairing-code-box').innerText.trim();
			if (!code || code === '--------') {
				showToast('Kode belum tersedia. Tekan Minta Kode Baru.');
				return;
			}
			navigator.clipboard.writeText(code).then(() => {
				showToast('✓ Kode pairing disalin: ' + code);
			});
		}

		async function requestNewPairing() {
			const phone = document.getElementById('phone-input').value.trim();
			if (!phone) {
				alert('Harap masukkan nomor WhatsApp lengkap dengan kode negara (contoh: 6281234567890)');
				return;
			}

			const key = activeAPIKey || document.getElementById('api-key-input')?.value.trim() || '';
			const btn = event.target;
			btn.disabled = true;
			btn.innerText = 'Meminta...';

			try {
				const headers = { 'Content-Type': 'application/json' };
				if (key) headers['X-API-Key'] = key;

				const res = await fetch('/api/v1/session/pair', {
					method: 'POST',
					headers: headers,
					body: JSON.stringify({ phone_number: phone })
				});

				const data = await res.json();
				if (res.ok && data.pairing_code) {
					document.getElementById('pairing-code-box').innerText = data.pairing_code;
					showToast('✓ Kode pairing baru: ' + data.pairing_code);
				} else {
					alert('Gagal meminta kode pairing: ' + (data.error || res.statusText));
				}
			} catch (err) {
				alert('Gagal terhubung ke server: ' + err.message);
			} finally {
				btn.disabled = false;
				btn.innerText = 'Minta Kode';
			}
		}

		async function disconnectSession() {
			if (!confirm('Apakah Anda yakin ingin memutuskan sesi WhatsApp ini?')) return;
			const key = activeAPIKey || document.getElementById('api-key-input')?.value.trim() || '';
			const headers = {};
			if (key) headers['X-API-Key'] = key;

			try {
				const res = await fetch('/api/v1/session/disconnect', { method: 'POST', headers: headers });
				if (res.ok) {
					alert('Sesi berhasil diputuskan. Halaman akan dimuat ulang.');
					window.location.reload();
				}
			} catch (err) {
				alert('Gagal memutuskan sesi: ' + err.message);
			}
		}

		// Background Poller: Cek status koneksi tiap 3 detik tanpa refresh reload
		setInterval(async () => {
			try {
				const key = activeAPIKey || localStorage.getItem('wa_api_key') || '';
				const headers = {};
				if (key) headers['X-API-Key'] = key;

				const res = await fetch('/api/v1/session/status', { headers: headers });
				if (!res.ok) return;
				const data = await res.json();

				if (data.is_connected && data.is_logged_in) {
					document.getElementById('waiting-view').style.display = 'none';
					document.getElementById('connected-view').style.display = 'block';
					if (data.phone_number) document.getElementById('disp-phone').innerText = data.phone_number;
					if (data.device_jid) document.getElementById('disp-jid').innerText = data.device_jid;
				} else {
					document.getElementById('connected-view').style.display = 'none';
					document.getElementById('waiting-view').style.display = 'block';
				}
			} catch (e) {}
		}, 3000);
	</script>
</body>
</html>`,
		boolToDisplay(isLoggedIn),
		status.PhoneNumber,
		status.Platform,
		status.DeviceJID,
		status.Uptime,
		boolToDisplay(!isLoggedIn),
		fallbackCode(initialCode),
		status.PhoneNumber,
		qrURL,
		defaultAPIKey,
		defaultAPIKey,
	)
}

func boolToDisplay(b bool) string {
	if b {
		return "block"
	}
	return "none"
}

func fallbackCode(code string) string {
	if code == "" {
		return "--------"
	}
	return code
}
