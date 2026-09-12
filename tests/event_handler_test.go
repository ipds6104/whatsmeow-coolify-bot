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

func TestEventHandler_QuotedMessageForwarding(t *testing.T) {
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

	// Send message replying to an earlier message
	evtHandler.HandleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     userJID,
				Sender:   userJID,
				IsFromMe: false,
			},
			ID:        "REPLY_MSG_001",
			Timestamp: time.Now(),
			PushName:  "Ihza",
		},
		Message: &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("Ini balasan untuk chat sebelumnya!"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:    proto.String("ORIGINAL_MSG_001"),
					Participant: proto.String("628982157341@s.whatsapp.net"),
					QuotedMessage: &waE2E.Message{
						Conversation: proto.String("Daftar tool Aina yang tersedia: ..."),
					},
					MentionedJID: []string{"628982157341@s.whatsapp.net"},
				},
			},
		},
	})

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(receivedPayloads) != 1 {
		t.Fatalf("expected exactly 1 forwarded message, got %d", len(receivedPayloads))
	}

	payload := receivedPayloads[0]
	if payload["id"] != "REPLY_MSG_001" {
		t.Errorf("expected id REPLY_MSG_001, got %v", payload["id"])
	}

	quoted, ok := payload["quoted_message"].(map[string]interface{})
	if !ok || quoted == nil {
		t.Fatalf("expected quoted_message in payload, got %v", payload["quoted_message"])
	}

	if quoted["id"] != "ORIGINAL_MSG_001" {
		t.Errorf("expected quoted id ORIGINAL_MSG_001, got %v", quoted["id"])
	}
	if quoted["text"] != "Daftar tool Aina yang tersedia: ..." {
		t.Errorf("expected quoted text, got %v", quoted["text"])
	}
	if quoted["sender"] != "628982157341@s.whatsapp.net" {
		t.Errorf("expected quoted sender 628982157341@s.whatsapp.net, got %v", quoted["sender"])
	}

	mentions, ok := payload["mentioned_jids"].([]interface{})
	if !ok || len(mentions) != 1 || mentions[0] != "628982157341@s.whatsapp.net" {
		t.Errorf("expected mentioned_jids to contain 628982157341@s.whatsapp.net, got %v", payload["mentioned_jids"])
	}
}

func TestEventHandler_LIDMentionDetection(t *testing.T) {
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

	groupJID := types.NewJID("6289625345646-1572457826", types.GroupServer)
	userJID := types.NewJID("87097809592405", "lid")

	// Send message in group mentioning the bot's LID (109389310636200@lid)
	evtHandler.HandleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     groupJID,
				Sender:   userJID,
				IsFromMe: false,
				IsGroup:  true,
			},
			ID:        "GROUP_LID_MSG_001",
			Timestamp: time.Now(),
			PushName:  "Ihza",
		},
		Message: &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("@109389310636200"),
				ContextInfo: &waE2E.ContextInfo{
					MentionedJID: []string{"109389310636200@lid"},
				},
			},
		},
	})

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(receivedPayloads) != 1 {
		t.Fatalf("expected 1 forwarded message, got %d", len(receivedPayloads))
	}

	payload := receivedPayloads[0]
	if payload["is_bot_mentioned"] != true {
		t.Errorf("expected is_bot_mentioned true, got %v", payload["is_bot_mentioned"])
	}
	if payload["bot_lid"] == nil || payload["bot_lid"] == "" {
		t.Errorf("expected bot_lid in payload, got %v", payload["bot_lid"])
	}
}

