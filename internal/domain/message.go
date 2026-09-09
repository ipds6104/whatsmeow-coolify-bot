package domain

import "time"

// MediaType represents supported WhatsApp media formats.
type MediaType string

const (
	MediaTypeImage    MediaType = "image"
	MediaTypeVideo    MediaType = "video"
	MediaTypeAudio    MediaType = "audio"
	MediaTypeVoice    MediaType = "voice"
	MediaTypeDocument MediaType = "document"
	MediaTypeSticker  MediaType = "sticker"
)

// TextMessage represents a payload for sending text to a contact or group.
type TextMessage struct {
	Recipient string `json:"recipient"`          // Phone number (e.g. 6281234567890) or Group JID (xxx@g.us)
	Content   string `json:"content"`            // Message body
	ReplyToID string `json:"reply_to_id,omitempty"` // Optional message ID being quoted
}

// MediaMessage represents a payload for sending media files to WhatsApp.
type MediaMessage struct {
	Recipient string    `json:"recipient"`
	Type      MediaType `json:"type"`
	FileName  string    `json:"file_name"`
	MimeType  string    `json:"mime_type"`
	Caption   string    `json:"caption,omitempty"`
	Data      []byte    `json:"-"` // Binary payload (not rendered in JSON)
	DataB64   string    `json:"data_base64,omitempty"`
	ReplyToID string    `json:"reply_to_id,omitempty"`
}

// ChatMessage represents a received or archived WhatsApp message.
type ChatMessage struct {
	ID        string                 `json:"id"`
	ChatJID   string                 `json:"chat_jid"`
	SenderJID string                 `json:"sender_jid"`
	SenderName string                `json:"sender_name,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	IsFromMe  bool                   `json:"is_from_me"`
	Text      string                 `json:"text,omitempty"`
	MediaType string                 `json:"media_type,omitempty"`
	HasMedia  bool                   `json:"has_media"`
	MediaInfo *MediaDownloadInfo     `json:"media_info,omitempty"`
	RawData   map[string]interface{} `json:"raw_data,omitempty"`
}

// MediaDownloadInfo holds decryption parameters needed by whatsmeow to download encrypted media.
type MediaDownloadInfo struct {
	DirectPath  string `json:"direct_path"`
	EncFileHash string `json:"enc_file_hash"`
	FileSha256  string `json:"file_sha256"`
	MediaKey    string `json:"media_key"`
	FileLength  uint64 `json:"file_length"`
	MimeType    string `json:"mime_type"`
	MediaType   string `json:"media_type"`
}
