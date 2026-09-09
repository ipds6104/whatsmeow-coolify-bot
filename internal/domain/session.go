package domain

import "time"

// SessionState represents the operational status of the WhatsApp client.
type SessionState string

const (
	StateDisconnected SessionState = "DISCONNECTED"
	StateConnecting   SessionState = "CONNECTING"
	StateConnected    SessionState = "CONNECTED"
	StatePairing        SessionState = "PAIRING"
	StateWaitingPairing SessionState = "WAITING_PAIRING"
	StateLoggedOut      SessionState = "LOGGED_OUT"
)

// SessionStatus holds comprehensive runtime details about the current WhatsApp session.
type SessionStatus struct {
	State        SessionState `json:"state"`
	IsConnected  bool         `json:"is_connected"`
	IsLoggedIn   bool         `json:"is_logged_in"`
	DeviceJID    string       `json:"device_jid,omitempty"`
	PhoneNumber  string       `json:"phone_number,omitempty"`
	Platform     string       `json:"platform,omitempty"`
	StartedAt    time.Time    `json:"started_at"`
	Uptime       string       `json:"uptime"`
	LastSeen     time.Time    `json:"last_seen,omitempty"`
	LastPairCode string       `json:"last_pair_code,omitempty"`
	ActionNeeded string       `json:"action_needed,omitempty"`
}

// PairingRequest defines the input required to initiate phone number pairing.
type PairingRequest struct {
	PhoneNumber string `json:"phone_number"` // Format: 6281234567890
	ClientName  string `json:"client_name,omitempty"`
}

// QRCodeResult encapsulates live WhatsApp QR code state.
type QRCodeResult struct {
	QRCode       string `json:"qr_code"`
	IsLoggedIn   bool   `json:"is_logged_in"`
	ExpiresInSec int    `json:"expires_in_sec"`
	PairingCode  string `json:"pairing_code,omitempty"`
}
