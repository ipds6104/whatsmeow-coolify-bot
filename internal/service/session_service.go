package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

// SessionServiceImpl implements ports.SessionService for WhatsApp lifecycle orchestration.
type SessionServiceImpl struct {
	client       ports.WhatsAppClientPort
	notifier     ports.NotifierPort
	store        ports.SessionStorePort
	phoneNumber  string
	clientName   string
	startedAt    time.Time
	needsPairing chan struct{}
	lastPairCode string
	lastQRCode   string
	qrExpiresAt  time.Time
	lastSeen     time.Time
	mu           sync.RWMutex
}

var _ ports.SessionService = (*SessionServiceImpl)(nil)

func NewSessionService(
	client ports.WhatsAppClientPort,
	notifier ports.NotifierPort,
	store ports.SessionStorePort,
	phoneNumber string,
	clientName string,
) *SessionServiceImpl {
	if clientName == "" {
		clientName = "Chrome (Linux)"
	}
	s := &SessionServiceImpl{
		client:       client,
		notifier:     notifier,
		store:        store,
		phoneNumber:  phoneNumber,
		clientName:   clientName,
		startedAt:    time.Now(),
		needsPairing: make(chan struct{}, 1),
	}

	return s
}

// Start checks whether a persistent session exists in Postgres. If not, it requests pairing.
func (s *SessionServiceImpl) Start(ctx context.Context) error {
	if !s.client.IsLoggedIn() {
		log.Println("Device belum ter-link di Postgres, memulai alur pairing code...")
		if s.phoneNumber != "" {
			go func() {
				if err := s.requestPairingWithRetry(context.Background(), s.phoneNumber); err != nil {
					log.Printf("initial pairing failed: %v", err)
				}
			}()
		}
		return nil
	}

	log.Println("Sesi ditemukan di Postgres store, menyambungkan ulang...")
	return s.client.Connect()
}

// GetStatus returns the current runtime connection and session diagnostics.
func (s *SessionServiceImpl) GetStatus(ctx context.Context) (domain.SessionStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := domain.StateDisconnected
	var actionNeeded string

	if s.client.IsConnected() && s.client.IsLoggedIn() {
		state = domain.StateConnected
	} else if !s.client.IsLoggedIn() {
		state = domain.StateWaitingPairing
		if s.lastPairCode != "" {
			actionNeeded = fmt.Sprintf("Buka WhatsApp HP ➔ Perangkat Tertaut ➔ Tautkan dengan nomor telepon ➔ Masukkan kode: %s", s.lastPairCode)
		} else {
			actionNeeded = "Menunggu kode pairing. Pantau channel Discord Anda atau panggil POST /api/v1/session/pair"
		}
	} else if s.client.IsConnected() {
		state = domain.StateConnected
	}

	return domain.SessionStatus{
		State:        state,
		IsConnected:  s.client.IsConnected(),
		IsLoggedIn:   s.client.IsLoggedIn(),
		DeviceJID:    s.client.GetDeviceJID(),
		PhoneNumber:  s.phoneNumber,
		Platform:     s.clientName,
		StartedAt:    s.startedAt,
		Uptime:       time.Since(s.startedAt).Round(time.Second).String(),
		LastSeen:     s.lastSeen,
		LastPairCode: s.lastPairCode,
		ActionNeeded: actionNeeded,
	}, nil
}

// SetQRCode updates the live QR code and its expiration timestamp.
func (s *SessionServiceImpl) SetQRCode(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastQRCode = code
	s.qrExpiresAt = time.Now().Add(25 * time.Second)
}

// GetQRCode returns the active QR code or status indicating if client is already paired.
func (s *SessionServiceImpl) GetQRCode(ctx context.Context) (domain.QRCodeResult, error) {
	if s.client.IsLoggedIn() {
		return domain.QRCodeResult{
			IsLoggedIn: true,
		}, nil
	}

	// Reconnect if socket closed so WhatsApp generates fresh QR codes
	if !s.client.IsConnected() {
		log.Println("[WHATSMEOW] Client disconnected saat request QR, menyambungkan ulang...")
		_ = s.client.Connect()
		for i := 0; i < 15; i++ {
			time.Sleep(100 * time.Millisecond)
			s.mu.RLock()
			fresh := time.Until(s.qrExpiresAt) > 0 && s.lastQRCode != ""
			s.mu.RUnlock()
			if fresh {
				break
			}
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	expiresIn := int(time.Until(s.qrExpiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}

	return domain.QRCodeResult{
		QRCode:       s.lastQRCode,
		IsLoggedIn:   false,
		ExpiresInSec: expiresIn,
		PairingCode:  s.lastPairCode,
	}, nil
}

// RequestPairing allows manual invocation of pairing code via REST API.
func (s *SessionServiceImpl) RequestPairing(ctx context.Context, req domain.PairingRequest) (string, error) {
	if req.PhoneNumber == "" {
		return "", domain.ErrInvalidPhoneNumber
	}

	s.mu.Lock()
	s.phoneNumber = req.PhoneNumber
	if req.ClientName != "" {
		s.clientName = req.ClientName
	}
	s.mu.Unlock()

	if !s.client.IsConnected() {
		if err := s.client.Connect(); err != nil {
			return "", fmt.Errorf("failed to connect to WhatsApp before pairing: %w", err)
		}
		time.Sleep(1500 * time.Millisecond)
	}

	code, err := s.client.PairPhone(ctx, req.PhoneNumber, s.clientName)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate-overlimit") {
			return "", fmt.Errorf("WhatsApp rate-limit (429): server WhatsApp membatasi permintaan kode untuk nomor ini. Mohon tunggu cooldown ~5-10 menit sebelum mencoba lagi")
		}
		return "", fmt.Errorf("pairing request failed: %w", err)
	}

	s.mu.Lock()
	s.lastPairCode = code
	s.mu.Unlock()

	_ = s.notifier.NotifyPairingCode(ctx, code, 1)
	return code, nil
}

// Disconnect safely terminates the WhatsApp websocket connection.
func (s *SessionServiceImpl) Disconnect(ctx context.Context) error {
	s.client.Disconnect()
	return s.notifier.Notify(ctx, ":wave: WhatsApp client disconnected gracefully.")
}

// TriggerLoggedOut notifies the supervisor loop that WhatsApp forced a logout.
func (s *SessionServiceImpl) TriggerLoggedOut(reason string) {
	s.mu.Lock()
	s.lastPairCode = ""
	s.mu.Unlock()

	_ = s.notifier.NotifyLogout(context.Background(), reason)
	select {
	case s.needsPairing <- struct{}{}:
	default:
	}
}

// UpdateLastSeen records the timestamp of a healthy connection or event.
func (s *SessionServiceImpl) UpdateLastSeen() {
	s.mu.Lock()
	s.lastSeen = time.Now()
	s.mu.Unlock()
}

// SupervisorLoop manages background connection recovery with exponential backoff and pairing retries.
func (s *SessionServiceImpl) SupervisorLoop(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 2 * time.Minute
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-s.needsPairing:
			if s.phoneNumber != "" {
				if err := s.requestPairingWithRetry(ctx, s.phoneNumber); err != nil {
					log.Printf("auto re-pairing failed: %v", err)
					_ = s.notifier.Notify(ctx, ":x: Auto-pairing gagal berulang kali. Silakan picu ulang via REST API.")
				}
			}

		case <-ticker.C:
			if !s.client.IsLoggedIn() {
				if !s.client.IsConnected() {
					log.Println("Session waiting for pairing disconnected, reconnecting for fresh QR...")
					_ = s.client.Connect()
				}
				continue
			}

			if s.client.IsConnected() {
				s.UpdateLastSeen()
				backoff = time.Second // Reset backoff on healthy connection
				continue
			}

			log.Printf("WhatsApp client disconnected, reconnecting (backoff %s)...", backoff)
			if err := s.client.Connect(); err != nil {
				log.Printf("reconnect failed: %v", err)
				time.Sleep(backoff)
				if backoff < maxBackoff {
					backoff *= 2
				}
			} else {
				backoff = time.Second
				s.UpdateLastSeen()
			}
		}
	}
}

func (s *SessionServiceImpl) requestPairingWithRetry(ctx context.Context, phone string) error {
	const maxAttempts = 20
	interval := 3 * time.Minute

	_ = s.notifier.Notify(ctx, fmt.Sprintf("⏳ **Menginisialisasi WhatsApp Pairing...**\nNomor: `%s`\nSedang meminta kode pairing 8-digit ke server WhatsApp...", phone))

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !s.client.IsConnected() {
			if err := s.client.Connect(); err != nil {
				log.Printf("connect before pair failed: %v", err)
				time.Sleep(10 * time.Second)
				continue
			}
			time.Sleep(1500 * time.Millisecond)
		}

		code, err := s.client.PairPhone(ctx, phone, s.clientName)
		if err != nil {
			log.Printf("PairPhone attempt %d error: %v", attempt, err)
			errStr := err.Error()
			if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate-overlimit") {
				_ = s.notifier.Notify(ctx, fmt.Sprintf("⏳ **WhatsApp Rate Limit (429 Cooldown)**: WhatsApp server membatasi frekuensi pairing untuk nomor `%s`.\nSistem otomatis menunggu cooldown 5 menit agar WhatsApp membuka blokir kembali...", phone))
				time.Sleep(5 * time.Minute)
			} else {
				_ = s.notifier.Notify(ctx, fmt.Sprintf("⚠️ Percobaan pairing #%d belum berhasil: `%v`. Mencoba ulang dalam 30 detik...", attempt, err))
				time.Sleep(30 * time.Second)
			}
			continue
		}

		s.mu.Lock()
		s.lastPairCode = code
		s.mu.Unlock()

		_ = s.notifier.NotifyPairingCode(ctx, code, attempt)

		waitUntil := time.Now().Add(interval)
		for time.Now().Before(waitUntil) {
			if s.client.IsLoggedIn() {
				log.Println("Pairing berhasil terkonfirmasi!")
				_ = s.notifier.Notify(ctx, ":tada: **WhatsApp berhasil ditautkan!** Bot kini aktif dan siap menerima pesan/perintah.")
				return nil
			}
			time.Sleep(5 * time.Second)
		}
	}

	return domain.ErrPairingTimeout
}

func (s *SessionServiceImpl) GetDeviceJID() string {
	return s.client.GetDeviceJID()
}

func (s *SessionServiceImpl) GetDeviceLID() string {
	return s.client.GetDeviceLID()
}
