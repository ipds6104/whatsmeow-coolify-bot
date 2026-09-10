package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/antiban"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/backup"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/rest"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/service"
)

func setupTestRouter(t *testing.T, apiKey string) http.Handler {
	tempDir, err := os.MkdirTemp("", "wa_rest_test_*")
	if err != nil {
		t.Fatalf("temp dir error: %v", err)
	}

	mockCli := &MockClient{connected: true, loggedIn: true}
	mockNotif := &MockNotifier{}
	mockStore := &MockStore{
		messages: []domain.ChatMessage{
			{ID: "1", ChatJID: "group1@g.us", Text: "Hello group"},
		},
	}
	guard := antiban.NewGuard("relaxed")
	fileStore, err := backup.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("file store error: %v", err)
	}

	sessionSvc := service.NewSessionService(mockCli, mockNotif, mockStore, "6281234567890", "TestBot")
	waSvc := service.NewWhatsAppService(mockCli, guard, mockStore)
	backupSvc := service.NewBackupService(mockCli, mockStore, fileStore)

	return rest.NewRouter(rest.RouterConfig{
		APIKey:          apiKey,
		SessionService:  sessionSvc,
		WhatsAppService: waSvc,
		BackupService:   backupSvc,
	})
}

func TestREST_Healthz(t *testing.T) {
	router := setupTestRouter(t, "secret-key-123")

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got '%s'", w.Body.String())
	}
}

func TestREST_AuthEnforcement(t *testing.T) {
	router := setupTestRouter(t, "secret-key-123")

	// 1. Without API Key -> 401 Unauthorized
	reqUnauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/session/status", nil)
	wUnauth := httptest.NewRecorder()
	router.ServeHTTP(wUnauth, reqUnauthorized)

	if wUnauth.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", wUnauth.Code)
	}

	// 2. With X-API-Key Header -> 200 OK
	reqAuthorized := httptest.NewRequest(http.MethodGet, "/api/v1/session/status", nil)
	reqAuthorized.Header.Set("X-API-Key", "secret-key-123")
	wAuth := httptest.NewRecorder()
	router.ServeHTTP(wAuth, reqAuthorized)

	if wAuth.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", wAuth.Code)
	}
}

func TestREST_SendTextMessage(t *testing.T) {
	router := setupTestRouter(t, "")

	payload := domain.TextMessage{
		Recipient: "6281234567890@s.whatsapp.net",
		Content:   "Pesan uji coba via REST API",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages/send-text", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp rest.APIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success true, got false")
	}
}

func TestREST_ListGroups(t *testing.T) {
	router := setupTestRouter(t, "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestREST_CreateGroupBackup(t *testing.T) {
	router := setupTestRouter(t, "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/groups/12345@g.us/backup", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestREST_GetQRCode(t *testing.T) {
	router := setupTestRouter(t, "secret-key-123")

	// Test JSON format
	req := httptest.NewRequest(http.MethodGet, "/api/v1/session/qr?format=json", nil)
	req.Header.Set("X-API-Key", "secret-key-123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Test HTML format
	reqHTML := httptest.NewRequest(http.MethodGet, "/api/v1/session/qr", nil)
	reqHTML.Header.Set("X-API-Key", "secret-key-123")
	reqHTML.Header.Set("Accept", "text/html")
	wHTML := httptest.NewRecorder()
	router.ServeHTTP(wHTML, reqHTML)

	if wHTML.Code != http.StatusOK {
		t.Errorf("expected status 200 for HTML view, got %d", wHTML.Code)
	}
}

func TestREST_GetAntiBanStats(t *testing.T) {
	router := setupTestRouter(t, "secret-key-123")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/antiban/stats", nil)
	req.Header.Set("X-API-Key", "secret-key-123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}

	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data field in response")
	}

	if data["preset"] != "relaxed" {
		t.Errorf("expected preset relaxed, got %v", data["preset"])
	}
}
