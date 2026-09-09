package domain

import "errors"

var (
	ErrNotConnected         = errors.New("whatsapp client is not connected")
	ErrNotLoggedIn          = errors.New("whatsapp client is not logged in / paired")
	ErrAlreadyConnected     = errors.New("whatsapp client is already connected")
	ErrPairingTimeout       = errors.New("maximum pairing code retry attempts exceeded")
	ErrInvalidPhoneNumber   = errors.New("invalid phone number format, must be E.164 without '+' prefix (e.g. 6281234567890)")
	ErrRateLimitExceeded    = errors.New("antiban guard: rate limit exceeded, message rejected")
	ErrWarmupCapReached     = errors.New("antiban guard: daily warmup message quota reached for current phase")
	ErrInvalidRecipient     = errors.New("invalid recipient JID or phone number")
	ErrGroupNotFound        = errors.New("group not found or bot is not a member")
	ErrMediaDownloadFailed  = errors.New("failed to download whatsapp media")
	ErrBackupNotFound       = errors.New("backup record not found")
	ErrUnauthorized         = errors.New("unauthorized: invalid or missing API key")
)
