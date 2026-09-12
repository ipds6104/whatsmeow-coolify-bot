package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

// Store provides persistence for chat messages and tracks whatsmeow session existence.
type Store struct {
	db *sql.DB
}

var _ ports.SessionStorePort = (*Store)(nil)

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Migrate creates necessary tables and indexes for message persistence and backup queries.
func (s *Store) Migrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id VARCHAR(128) PRIMARY KEY,
			chat_jid VARCHAR(128) NOT NULL,
			sender_jid VARCHAR(128) NOT NULL,
			sender_name TEXT,
			timestamp TIMESTAMPTZ NOT NULL,
			is_from_me BOOLEAN NOT NULL,
			text TEXT,
			media_type VARCHAR(64),
			has_media BOOLEAN NOT NULL DEFAULT FALSE,
			media_info JSONB,
			raw_data JSONB,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_chat_ts ON chat_messages(chat_jid, timestamp DESC)`,
	}
	for i, q := range queries {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("postgres migration query %d failed: %w", i+1, err)
		}
	}
	log.Println("[MIGRATE] Database chat_messages schema ready")
	return nil
}

// SaveMessage stores an incoming or outgoing message into the Postgres store.
func (s *Store) SaveMessage(ctx context.Context, msg domain.ChatMessage) error {
	mediaJSON, err := json.Marshal(msg.MediaInfo)
	if err != nil {
		mediaJSON = []byte("{}")
	}

	rawJSON, err := json.Marshal(msg.RawData)
	if err != nil {
		rawJSON = []byte("{}")
	}

	query := `
	INSERT INTO chat_messages (id, chat_jid, sender_jid, sender_name, timestamp, is_from_me, text, media_type, has_media, media_info, raw_data)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	ON CONFLICT (id) DO UPDATE SET
		text = EXCLUDED.text,
		media_type = EXCLUDED.media_type,
		has_media = EXCLUDED.has_media,
		media_info = EXCLUDED.media_info;
	`
	_, err = s.db.ExecContext(ctx, query,
		msg.ID,
		msg.ChatJID,
		msg.SenderJID,
		msg.SenderName,
		msg.Timestamp,
		msg.IsFromMe,
		msg.Text,
		msg.MediaType,
		msg.HasMedia,
		mediaJSON,
		rawJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to save message %s: %w", msg.ID, err)
	}
	return nil
}

// GetMessages queries recent messages from a specific chat or group in descending order.
func (s *Store) GetMessages(ctx context.Context, chatJID string, limit int) ([]domain.ChatMessage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	query := `
	SELECT id, chat_jid, sender_jid, COALESCE(sender_name, ''), timestamp, is_from_me, COALESCE(text, ''), COALESCE(media_type, ''), has_media, media_info, raw_data
	FROM chat_messages
	WHERE chat_jid = $1
	ORDER BY timestamp DESC
	LIMIT $2;
	`

	rows, err := s.db.QueryContext(ctx, query, chatJID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages for %s: %w", chatJID, err)
	}
	defer rows.Close()

	var messages []domain.ChatMessage
	for rows.Next() {
		var msg domain.ChatMessage
		var mediaJSON, rawJSON []byte

		err := rows.Scan(
			&msg.ID,
			&msg.ChatJID,
			&msg.SenderJID,
			&msg.SenderName,
			&msg.Timestamp,
			&msg.IsFromMe,
			&msg.Text,
			&msg.MediaType,
			&msg.HasMedia,
			&mediaJSON,
			&rawJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan message error: %w", err)
		}

		if len(mediaJSON) > 0 && string(mediaJSON) != "null" {
			var info domain.MediaDownloadInfo
			if err := json.Unmarshal(mediaJSON, &info); err == nil {
				msg.MediaInfo = &info
			}
		}

		if len(rawJSON) > 0 && string(rawJSON) != "null" {
			var raw map[string]interface{}
			if err := json.Unmarshal(rawJSON, &raw); err == nil {
				msg.RawData = raw
			}
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// GetAllMessages queries recent messages across all chats in descending order.
func (s *Store) GetAllMessages(ctx context.Context, limit int) ([]domain.ChatMessage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	query := `
	SELECT id, chat_jid, sender_jid, COALESCE(sender_name, ''), timestamp, is_from_me, COALESCE(text, ''), COALESCE(media_type, ''), has_media, media_info, raw_data
	FROM chat_messages
	ORDER BY timestamp DESC
	LIMIT $1;
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query all messages: %w", err)
	}
	defer rows.Close()

	var messages []domain.ChatMessage
	for rows.Next() {
		var msg domain.ChatMessage
		var mediaJSON, rawJSON []byte

		err := rows.Scan(
			&msg.ID,
			&msg.ChatJID,
			&msg.SenderJID,
			&msg.SenderName,
			&msg.Timestamp,
			&msg.IsFromMe,
			&msg.Text,
			&msg.MediaType,
			&msg.HasMedia,
			&mediaJSON,
			&rawJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan message error: %w", err)
		}

		if len(mediaJSON) > 0 && string(mediaJSON) != "null" {
			var info domain.MediaDownloadInfo
			if err := json.Unmarshal(mediaJSON, &info); err == nil {
				msg.MediaInfo = &info
			}
		}

		if len(rawJSON) > 0 && string(rawJSON) != "null" {
			var raw map[string]interface{}
			if err := json.Unmarshal(rawJSON, &raw); err == nil {
				msg.RawData = raw
			}
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// HasSession checks if whatsmeow has an active registered device session in the database.
func (s *Store) HasSession(ctx context.Context) (bool, error) {
	var count int
	// whatsmeow creates "whatsmeow_device" table
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'whatsmeow_device'").Scan(&count)
	if err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}

	var deviceCount int
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM whatsmeow_device").Scan(&deviceCount)
	if err != nil {
		return false, err
	}

	return deviceCount > 0, nil
}
