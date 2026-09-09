package service

import (
	"context"
	"fmt"
	"log"
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
		clientName = "Chrome (Coolify)"
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
	if s.client.IsConnected() {
		state = domain.StateConnected
	} else if len(s.needsPairing) > 0 {
		state = domain.StatePairing
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

	code, err := s.client.PairPhone(ctx, req.PhoneNumber, s.clientName)
	if err != nil {
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
				continue // Waiting for user pairing
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
		}

		code, err := s.client.PairPhone(ctx, phone, s.clientName)
		if err != nil {
			log.Printf("PairPhone attempt %d error: %v", attempt, err)
			time.Sleep(30 * time.Second)
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
				_ = s.notifier.Notify(ctx, ":tada: WhatsApp berhasil di-link!")
				return nil
			}
			time.Sleep(5 * time.Second)
		}
	}

	return domain.ErrPairingTimeout
}
