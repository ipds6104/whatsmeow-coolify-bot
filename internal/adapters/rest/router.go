package rest

import (
	"net/http"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

type RouterConfig struct {
	APIKey         string
	SessionService ports.SessionService
	WhatsAppService ports.WhatsAppService
	BackupService  ports.BackupService
}

// NewRouter constructs the standard Go HTTP router with middleware chain and registered API routes.
func NewRouter(cfg RouterConfig) http.Handler {
	mux := http.NewServeMux()

	sessionH := NewSessionHandler(cfg.SessionService)
	msgH := NewMessageHandler(cfg.WhatsAppService)
	groupH := NewGroupHandler(cfg.WhatsAppService)
	backupH := NewBackupHandler(cfg.BackupService)

	// Healthcheck endpoint (Unauthenticated for Traefik / Coolify health monitoring)
	mux.HandleFunc("GET /healthz", sessionH.Healthz)

	// Session management endpoints
	mux.HandleFunc("GET /api/v1/session/status", sessionH.GetStatus)
	mux.HandleFunc("POST /api/v1/session/pair", sessionH.RequestPairing)
	mux.HandleFunc("POST /api/v1/session/disconnect", sessionH.Disconnect)

	// Messaging endpoints
	mux.HandleFunc("POST /api/v1/messages/send-text", msgH.SendText)
	mux.HandleFunc("POST /api/v1/messages/send-media", msgH.SendMedia)
	mux.HandleFunc("POST /api/v1/media/download", msgH.DownloadMedia)
	mux.HandleFunc("GET /api/v1/chats/{jid}/messages", msgH.GetChatMessages)

	// Group management endpoints
	mux.HandleFunc("GET /api/v1/groups", groupH.ListGroups)
	mux.HandleFunc("GET /api/v1/groups/{jid}", groupH.GetGroupInfo)

	// Backup and export endpoints
	mux.HandleFunc("POST /api/v1/groups/{jid}/backup", backupH.CreateBackup)
	mux.HandleFunc("POST /api/v1/backups", backupH.CreateBackup)
	mux.HandleFunc("GET /api/v1/backups", backupH.ListBackups)
	mux.HandleFunc("GET /api/v1/backups/{id}", backupH.DownloadBackup)

	// Wrap middleware chain: Recovery -> Logger -> CORS -> Auth
	handler := CORSMiddleware(mux)
	handler = AuthMiddleware(cfg.APIKey, handler)
	handler = LoggerMiddleware(handler)
	handler = RecoveryMiddleware(handler)

	return handler
}
