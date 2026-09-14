package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

// Notifier sends formatted alert webhooks and pairing codes to a Discord channel.
type Notifier struct {
	webhookURL  string
	coolifyFQDN string
	apiKey      string
	httpClient  *http.Client
}

// Ensure Notifier implements ports.NotifierPort
var _ ports.NotifierPort = (*Notifier)(nil)

func NewNotifier(webhookURL string, coolifyFQDN string, apiKey string) *Notifier {
	return &Notifier{
		webhookURL:  webhookURL,
		coolifyFQDN: strings.TrimRight(coolifyFQDN, "/"),
		apiKey:      apiKey,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *Notifier) getWebPairingURL() string {
	baseURL := n.coolifyFQDN
	if baseURL == "" {
		baseURL = "http://localhost:3000"
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}
	if n.apiKey != "" {
		return fmt.Sprintf("%s/pair?key=%s", baseURL, url.QueryEscape(n.apiKey))
	}
	return baseURL + "/pair"
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

	webPairURL := n.getWebPairingURL()

	embed := discordEmbed{
		Title:       "🔑 KODE PAIRING WHATSAPP BARU",
		Description: fmt.Sprintf("Salin kode di bawah ini lalu masukkan ke WhatsApp HP Anda:\n\n# `%s`\n", code),
		Color:       0x25D366, // WhatsApp Green
		Fields: []discordField{
			{Name: "Percobaan", Value: fmt.Sprintf("#%d / 20", attempt), Inline: true},
			{Name: "Masa Berlaku", Value: "~3 Menit", Inline: true},
			{
				Name:   "📲 Tautan Cepat (Buka di HP)",
				Value:  fmt.Sprintf("[🌐 Buka Web Pairing & QR Scanner](%s) • [📲 Buka Perangkat Tertaut](https://wa.me/settings/linked_devices)", webPairURL),
				Inline: false,
			},
			{
				Name:   "📋 Langkah di WhatsApp HP",
				Value:  "Buka WA di HP ➔ **Setelan** ➔ **Perangkat Tertaut** ➔ **Tautkan dengan nomor telepon** ➔ Masukkan kode di atas",
				Inline: false,
			},
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
	log.Printf("[LOGOUT ALERT] WhatsApp logout detected: %s", reason)
	if n.webhookURL == "" {
		return nil
	}

	webPairURL := n.getWebPairingURL()

	embed := discordEmbed{
		Title:       "⚠️ WhatsApp Logout Paksa Terdeteksi",
		Description: fmt.Sprintf("Sesi WhatsApp terputus dari server.\n**Alasan**: `%s`\n\nSistem self-healing otomatis menginisialisasi ulang sesi perangkat dan meminta kode pairing baru ke WhatsApp server.", reason),
		Color:       0xED4245, // Red
		Fields: []discordField{
			{
				Name:   "🌐 Buka Web Dashboard Pairing",
				Value:  fmt.Sprintf("[📲 Klik Disini untuk Buka Web Pairing & QR Scanner](%s)", webPairURL),
				Inline: false,
			},
			{
				Name:   "📋 Panduan Pemulihan",
				Value:  "1. Pastikan nomor HP Anda aktif di aplikasi WhatsApp.\n2. Buka **Setelan ➔ Perangkat Tertaut ➔ Tautkan Perangkat**.\n3. Masukkan kode pairing baru atau scan QR code dari link di atas.",
				Inline: false,
			},
		},
		Footer:    &discordFooter{Text: "whatsmeow-coolify-bot • Self-Healing System"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	payload := webhookPayload{
		Content: "🚨 **PERINGATAN: Sesi WhatsApp Terputus (Perlu Ditautkan Ulang)**",
		Embeds:  []discordEmbed{embed},
	}

	return n.post(ctx, payload)
}

func (n *Notifier) NotifyAutoPairingExhausted(ctx context.Context, phone string) error {
	if n.webhookURL == "" {
		return nil
	}

	webPairURL := n.getWebPairingURL()

	embed := discordEmbed{
		Title:       "⏳ Batas Waktu Auto-Pairing Selesai",
		Description: fmt.Sprintf("Sistem telah mencoba menghasilkan kode pairing untuk nomor `%s`, namun kode belum sempat dikonfirmasi di HP.\n\nTidak perlu panik! Bot tetap standby. Anda dapat membuat kode baru atau memindai QR code kapan saja melalui Web Dashboard:", phone),
		Color:       0xFEE75C, // Yellow
		Fields: []discordField{
			{
				Name:   "🌐 Web Pairing Dashboard",
				Value:  fmt.Sprintf("[📲 Buka Web Dashboard Pairing](%s)", webPairURL),
				Inline: false,
			},
			{
				Name:   "💡 Tips Cepat",
				Value:  "Buka tautan di atas dari browser HP Anda, lalu tekan tombol **Minta Kode Baru** atau arahkan kamera WhatsApp ke QR Code.",
				Inline: false,
			},
		},
		Footer:    &discordFooter{Text: "whatsmeow-coolify-bot • Standby Mode"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	payload := webhookPayload{
		Content: "ℹ️ **Informasi Sesi: Menunggu Konfirmasi Penautan WhatsApp**",
		Embeds:  []discordEmbed{embed},
	}

	return n.post(ctx, payload)
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
