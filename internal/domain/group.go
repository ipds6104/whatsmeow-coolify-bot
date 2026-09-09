package domain

import "time"

// GroupParticipant represents a member in a WhatsApp group.
type GroupParticipant struct {
	JID          string `json:"jid"`
	PhoneNumber  string `json:"phone_number,omitempty"`
	IsAdmin      bool   `json:"is_admin"`
	IsSuperAdmin bool   `json:"is_super_admin"`
}

// GroupInfo represents rich metadata for a WhatsApp group.
type GroupInfo struct {
	JID           string             `json:"jid"`
	Name          string             `json:"name"`
	OwnerJID      string             `json:"owner_jid,omitempty"`
	Topic         string             `json:"topic,omitempty"`
	TopicSetBy    string             `json:"topic_set_by,omitempty"`
	TopicSetAt    time.Time          `json:"topic_set_at,omitempty"`
	CreatedAt     time.Time          `json:"created_at,omitempty"`
	IsAnnounce    bool               `json:"is_announce"`
	IsLocked      bool               `json:"is_locked"`
	MemberCount   int                `json:"member_count"`
	Participants  []GroupParticipant `json:"participants,omitempty"`
}

// BackupRequest specifies parameters for archiving a chat or group.
type BackupRequest struct {
	ChatJID      string `json:"chat_jid"`
	IncludeMedia bool   `json:"include_media"`
	Limit        int    `json:"limit"`        // Max messages to backup (default 500, 0 = unlimited)
	Format       string `json:"format"`       // json, markdown, txt
}

// BackupResult details the generated backup archive.
type BackupResult struct {
	BackupID      string    `json:"backup_id"`
	ChatJID       string    `json:"chat_jid"`
	ChatName      string    `json:"chat_name"`
	TotalMessages int       `json:"total_messages"`
	TotalMedia    int       `json:"total_media"`
	CreatedAt     time.Time `json:"created_at"`
	FilePath      string    `json:"file_path"`
	FileSize      int64     `json:"file_size"`
	Format        string    `json:"format"`
}
