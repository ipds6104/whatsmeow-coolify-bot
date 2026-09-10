package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

// Notifier sends formatted alert webhooks and pairing codes to a Discord channel.
type Notifier struct {
	webhookURL string
	httpClient *http.Client
}

// Ensure Notifier implements ports.NotifierPort
var _ ports.NotifierPort = (*Notifier)(nil)

func NewNotifier(webhookURL string) *Notifier {
	return &Notifier{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type discordEmbed struct {
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color,omitempty"`
	Fields      []discordField `json:"fields,omitempty"`
	Footer      *discordFooter `json:"footer,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type discordFooter struct {
	Text string `json:"text"`
}

type webhookPayload struct {
	Content string         `json:"content,omitempty"`
	Embeds  []discordEmbed `json:"embeds,omitempty"`
}

func (n *Notifier) Notify(ctx context.Context, message string) error {
	if n.webhookURL == "" {
		log.Printf("[NOTIFIER-CONSOLE] %s", message)
		return nil
	}

	payload := webhookPayload{
		Content: message,
	}
	return n.post(ctx, payload)
}

func (n *Notifier) NotifyPairingCode(ctx context.Context, code string, attempt int) error {
	log.Printf("[PAIRING CODE] Percobaan %d: %s", attempt, code)

	if n.webhookURL == "" {
		return nil
	}

	embed := discordEmbed{
		Title:       "🔑 KODE PAIRING WHATSAPP BARU",
		Description: fmt.Sprintf("Salin kode di bawah ini lalu masukkan ke WhatsApp HP Anda:\n\n# `%s`\n", code),
		Color:       0x25D366, // WhatsApp Green
		Fields: []discordField{
			{Name: "Percobaan", Value: fmt.Sprintf("#%d / 20", attempt), Inline: true},
			{Name: "Masa Berlaku", Value: "~3 Menit", Inline: true},
			{Name: "Langkah di HP", Value: "Buka WA di HP ➔ Setelan ➔ Perangkat Tertaut ➔ Tautkan dengan nomor telepon ➔ Masukkan kode di atas", Inline: false},
			{Name: "Tautan Cepat (One-Click)", Value: "[📲 Buka WhatsApp Perangkat Tertaut](https://wa.me/settings/linked_devices) • [📷 Scan QR Scanner Web](https://wa.dvlpid.my.id/api/v1/session/qr)", Inline: false},
		},
		Footer:    &discordFooter{Text: "whatsmeow-coolify-bot • Auto-Pairing System"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	payload := webhookPayload{
		Content: "🚨 **Tindakan Diperlukan: Tautkan WhatsApp Anda**",
		Embeds:  []discordEmbed{embed},
	}

	return n.post(ctx, payload)
}

func (n *Notifier) NotifyLogout(ctx context.Context, reason string) error {
	msg := fmt.Sprintf("⚠️ **WhatsApp Logout Paksa Terdeteksi!**\nAlasan: `%s`\nSistem sedang memulai generate kode pairing baru secara otomatis...", reason)
	return n.Notify(ctx, msg)
}

func (n *Notifier) post(ctx context.Context, payload webhookPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal discord payload failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create http request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post to discord webhook failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord webhook responded with status: %d", resp.StatusCode)
	}

	return nil
}
