package tests

import (
	"encoding/json"
	"fmt"
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

func TestEventHandler_RetryQueueDowntimeResilience(t *testing.T) {
	var mu sync.Mutex
	var receivedIDs []string
	isServerHealthy := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if !isServerHealthy {
			// Simulate Aina offline/restarting (502 Bad Gateway)
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
			return
		}

		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if id, ok := body["id"].(string); ok {
			receivedIDs = append(receivedIDs, id)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	mockCli := &MockClient{connected: true, loggedIn: true}
	mockNotif := &MockNotifier{}
	mockStore := &MockStore{}

	sessionService := service.NewSessionService(mockCli, mockNotif, mockStore, "628982157341", "Chrome (Linux)")
	evtHandler := waAdapter.NewEventHandler(sessionService, mockNotif, mockStore, server.URL, "primary_bot")
	defer evtHandler.Stop()

	userJID := types.NewJID("6289625345646", types.DefaultUserServer)

	// Send 3 messages while server is DOWN
	for i := 1; i <= 3; i++ {
		msgID := fmt.Sprintf("MSG_QUEUE_%03d", i)
		evtHandler.HandleEvent(&events.Message{
			Info: types.MessageInfo{
				MessageSource: types.MessageSource{
					Chat:     userJID,
					Sender:   userJID,
					IsFromMe: false,
				},
				ID:        msgID,
				Timestamp: time.Now(),
				PushName:  "Ihza",
			},
			Message: &waE2E.Message{
				Conversation: proto.String(fmt.Sprintf("Pesan ke-%d", i)),
			},
		})
		time.Sleep(50 * time.Millisecond)
	}

	// Verify all 3 messages entered retry queue or failed first attempt
	time.Sleep(200 * time.Millisecond)
	diag := evtHandler.GetDiagnostics()
	pendingCount, _ := diag["pending_retry_count"].(int)
	if pendingCount == 0 {
		t.Fatalf("expected messages to be in retry queue during downtime, got %d", pendingCount)
	}

	// Now recover the server (Aina comes back online)
	mu.Lock()
	isServerHealthy = true
	mu.Unlock()

	// Wait for retry worker to flush pending queue (runs every 2s)
	// Poll until queue is drained or timeout after 6 seconds
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(receivedIDs)
		mu.Unlock()
		if count == 3 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(receivedIDs) != 3 {
		t.Fatalf("expected all 3 messages delivered after recovery, got %d: %v", len(receivedIDs), receivedIDs)
	}

	// Strict FIFO verification: order must be MSG_QUEUE_001, MSG_QUEUE_002, MSG_QUEUE_003
	expectedOrder := []string{"MSG_QUEUE_001", "MSG_QUEUE_002", "MSG_QUEUE_003"}
	for i, exp := range expectedOrder {
		if receivedIDs[i] != exp {
			t.Errorf("expected receivedIDs[%d] = %s, got %s", i, exp, receivedIDs[i])
		}
	}
}

func TestEventHandler_413PayloadTooLarge_SelfHealing(t *testing.T) {
	var mu sync.Mutex
	var receivedPayloads []map[string]interface{}
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Unlock()

		// If media_base64 is present, simulate HTTP 413 Payload Too Large (Axum / Reverse proxy limit)
		if _, hasB64 := body["media_base64"]; hasB64 {
			http.Error(w, "Payload Too Large", http.StatusRequestEntityTooLarge)
			return
		}

		// Once media_base64 is stripped, accept the payload
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
	defer evtHandler.Stop()

	userJID := types.NewJID("6282234120921", types.DefaultUserServer)

	// Send message with image / large media
	evtHandler.HandleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     userJID,
				Sender:   userJID,
				IsFromMe: false,
			},
			ID:        "DOC_413_MSG_001",
			Timestamp: time.Now(),
			PushName:  "Rekan Kerja",
		},
		Message: &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				Caption: proto.String("ini pdfnya"),
			},
		},
	})

	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(receivedPayloads) != 1 {
		t.Fatalf("expected exactly 1 payload accepted after stripping media_base64, got %d (total attempts: %d)", len(receivedPayloads), attempts)
	}

	p := receivedPayloads[0]
	if p["id"] != "DOC_413_MSG_001" {
		t.Errorf("expected id DOC_413_MSG_001, got %v", p["id"])
	}
	if p["text"] != "ini pdfnya" {
		t.Errorf("expected text 'ini pdfnya', got %v", p["text"])
	}
	if p["media_base64_stripped"] != true {
		t.Errorf("expected media_base64_stripped true, got %v", p["media_base64_stripped"])
	}
	if _, hasB64 := p["media_base64"]; hasB64 {
		t.Errorf("expected media_base64 to be stripped from payload, but found it")
	}

	// Verify retry queue is EMPTY (no HoL blocking)
	diag := evtHandler.GetDiagnostics()
	pendingCount, _ := diag["pending_retry_count"].(int)
	if pendingCount != 0 {
		t.Errorf("expected pending_retry_count to be 0, got %d", pendingCount)
	}
}

func TestEventHandler_NonRetryable4xx_NoHoLBlocking(t *testing.T) {
	var mu sync.Mutex
	var receivedIDs []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		id, _ := body["id"].(string)

		mu.Lock()
		defer mu.Unlock()

		if id == "FAIL_400_MSG" {
			// Simulate 400 Bad Request
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		receivedIDs = append(receivedIDs, id)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	mockCli := &MockClient{connected: true, loggedIn: true}
	mockNotif := &MockNotifier{}
	mockStore := &MockStore{}

	sessionService := service.NewSessionService(mockCli, mockNotif, mockStore, "628982157341", "Chrome (Linux)")
	evtHandler := waAdapter.NewEventHandler(sessionService, mockNotif, mockStore, server.URL, "primary_bot")
	defer evtHandler.Stop()

	userJID := types.NewJID("6289625345646", types.DefaultUserServer)

	// 1. Send failing message that returns 400
	evtHandler.HandleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     userJID,
				Sender:   userJID,
				IsFromMe: false,
			},
			ID:        "FAIL_400_MSG",
			Timestamp: time.Now(),
			PushName:  "Test",
		},
		Message: &waE2E.Message{
			Conversation: proto.String("Pesan gagal"),
		},
	})

	time.Sleep(100 * time.Millisecond)

	// 2. Immediately send subsequent message ("Aina?")
	evtHandler.HandleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     userJID,
				Sender:   userJID,
				IsFromMe: false,
			},
			ID:        "AINA_MSG_002",
			Timestamp: time.Now(),
			PushName:  "Ihza",
		},
		Message: &waE2E.Message{
			Conversation: proto.String("Aina?"),
		},
	})

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// FAIL_400_MSG was dropped without entering retry queue.
	// AINA_MSG_002 must be delivered immediately without any delay!
	if len(receivedIDs) != 1 || receivedIDs[0] != "AINA_MSG_002" {
		t.Fatalf("expected AINA_MSG_002 delivered immediately without HoL blocking, got: %v", receivedIDs)
	}

	diag := evtHandler.GetDiagnostics()
	pendingCount, _ := diag["pending_retry_count"].(int)
	if pendingCount != 0 {
		t.Errorf("expected pending_retry_count to be 0, got %d", pendingCount)
	}
}


