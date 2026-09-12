package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	waAdapter "github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/whatsmeow"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/service"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestEventHandler_WebhookForwarding(t *testing.T) {
	var mu sync.Mutex
	var receivedPayloads []map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		receivedPayloads = append(receivedPayloads, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	mockCli := &MockClient{connected: true, loggedIn: true}
	mockNotif := &MockNotifier{}
	mockStore := &MockStore{}

	sessionService := service.NewSessionService(mockCli, mockNotif, mockStore, "628982157341", "Chrome (Linux)")
	evtHandler := waAdapter.NewEventHandler(sessionService, mockNotif, mockStore, server.URL, "primary_bot")

	userJID := types.NewJID("6289625345646", types.DefaultUserServer)
	botJID := types.NewJID("628982157341", types.DefaultUserServer)

	// 1. Incoming user message (is_from_me = false) -> should forward
	evtHandler.HandleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     userJID,
				Sender:   userJID,
				IsFromMe: false,
			},
			ID:        "MSG_001",
			Timestamp: time.Now(),
			PushName:  "Ihza",
		},
		Message: &waE2E.Message{
			Conversation: proto.String("Halo Aina!"),
		},
	})

	// 2. Outgoing bot message (is_from_me = true) -> should NOT forward
	evtHandler.HandleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     userJID,
				Sender:   botJID,
				IsFromMe: true,
			},
			ID:        "MSG_002",
			Timestamp: time.Now(),
			PushName:  "Aina",
		},
		Message: &waE2E.Message{
			Conversation: proto.String("Halo juga!"),
		},
	})

	// Wait briefly for async goroutine
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(receivedPayloads) != 1 {
		t.Fatalf("expected exactly 1 forwarded message, got %d", len(receivedPayloads))
	}

	payload := receivedPayloads[0]
	if payload["id"] != "MSG_001" {
		t.Errorf("expected id MSG_001, got %v", payload["id"])
	}
	if payload["text"] != "Halo Aina!" {
		t.Errorf("expected text 'Halo Aina!', got %v", payload["text"])
	}
	if payload["sender_name"] != "Ihza" {
		t.Errorf("expected sender_name 'Ihza', got %v", payload["sender_name"])
	}
	if payload["session_role"] != "primary_bot" {
		t.Errorf("expected session_role 'primary_bot', got %v", payload["session_role"])
	}
}
