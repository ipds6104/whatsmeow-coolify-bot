package domain

// ProfilePictureResult contains the resolved profile picture URL and metadata.
type ProfilePictureResult struct {
	JID  string `json:"jid"`
	URL  string `json:"url"`
	ID   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"`
}

// StatusBroadcastMessage represents an ephemeral WhatsApp 24-hour Status story.
type StatusBroadcastMessage struct {
	Text            string    `json:"text,omitempty"`
	Type            MediaType `json:"type,omitempty"` // "text", "image", "video"
	Caption         string    `json:"caption,omitempty"`
	FileName        string    `json:"file_name,omitempty"`
	MimeType        string    `json:"mime_type,omitempty"`
	Data            []byte    `json:"-"`
	DataB64         string    `json:"data_base64,omitempty"`
	BackgroundColor uint32    `json:"background_color,omitempty"` // ARGB format e.g. 0xFF25D366
	Font            int32     `json:"font,omitempty"`             // 1-5 fonts supported by WA Web
}

// AboutStatusRequest represents updating the user's permanent "About" status text.
type AboutStatusRequest struct {
	Status string `json:"status"`
}

// MessageRevokeRequest represents revoking (deleting for everyone) a sent message or status story.
type MessageRevokeRequest struct {
	ChatJID   string `json:"chat_jid"`
	MessageID string `json:"message_id"`
}
