package tests

import (
	"context"
	"testing"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/service"
)

// MockClient implements ports.WhatsAppClientPort for unit tests
type MockClient struct {
	connected bool
	loggedIn  bool
	pairCode  string
	pairErr   error
	connectErr error
	sentMsgs  []string
}

func (m *MockClient) Connect() error {
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *MockClient) Disconnect() {
	m.connected = false
}

func (m *MockClient) IsConnected() bool {
	return m.connected
}

func (m *MockClient) IsLoggedIn() bool {
	return m.loggedIn
}

func (m *MockClient) GetDeviceJID() string {
	return "6281234567890:1@s.whatsapp.net"
}

func (m *MockClient) PairPhone(ctx context.Context, phone string, clientDisplayName string) (string, error) {
	if m.pairErr != nil {
		return "", m.pairErr
	}
	m.pairCode = "1234-5678"
	return m.pairCode, nil
}

func (m *MockClient) SendTextMessage(ctx context.Context, to string, text string, replyID string) (string, error) {
	m.sentMsgs = append(m.sentMsgs, text)
	return "MSG_12345", nil
}

func (m *MockClient) SendMediaMessage(ctx context.Context, to string, media domain.MediaMessage) (string, error) {
	return "MEDIA_12345", nil
}

func (m *MockClient) DownloadMedia(ctx context.Context, info domain.MediaDownloadInfo) ([]byte, error) {
	return []byte("fake_media_data"), nil
}

func (m *MockClient) GetGroupInfo(ctx context.Context, groupJID string) (domain.GroupInfo, error) {
	return domain.GroupInfo{
		JID:  groupJID,
		Name: "Test Developer Group",
	}, nil
}

func (m *MockClient) GetJoinedGroups(ctx context.Context) ([]domain.GroupInfo, error) {
	return []domain.GroupInfo{
		{JID: "12345@g.us", Name: "Developer Team"},
	}, nil
}

func (m *MockClient) AddEventHandler(handler func(evt interface{})) {}

// MockNotifier implements ports.NotifierPort
type MockNotifier struct {
	notifs []string
	codes  []string
}

func (m *MockNotifier) Notify(ctx context.Context, message string) error {
	m.notifs = append(m.notifs, message)
	return nil
}

func (m *MockNotifier) NotifyPairingCode(ctx context.Context, code string, attempt int) error {
	m.codes = append(m.codes, code)
	return nil
}

func (m *MockNotifier) NotifyLogout(ctx context.Context, reason string) error {
	m.notifs = append(m.notifs, "logout: "+reason)
	return nil
}

// MockStore implements ports.SessionStorePort
type MockStore struct {
	messages []domain.ChatMessage
}

func (m *MockStore) SaveMessage(ctx context.Context, msg domain.ChatMessage) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *MockStore) GetMessages(ctx context.Context, chatJID string, limit int) ([]domain.ChatMessage, error) {
	return m.messages, nil
}

func (m *MockStore) HasSession(ctx context.Context) (bool, error) {
	return true, nil
}

func TestSessionService_ConnectWhenLoggedIn(t *testing.T) {
	mockCli := &MockClient{loggedIn: true}
	mockNotif := &MockNotifier{}
	mockStore := &MockStore{}

	svc := service.NewSessionService(mockCli, mockNotif, mockStore, "6281234567890", "TestBot")

	ctx := context.Background()
	err := svc.Start(ctx)
	if err != nil {
		t.Fatalf("expected start to succeed: %v", err)
	}

	if !mockCli.IsConnected() {
		t.Errorf("expected client to be connected")
	}

	status, err := svc.GetStatus(ctx)
	if err != nil {
		t.Fatalf("get status error: %v", err)
	}

	if !status.IsConnected || !status.IsLoggedIn {
		t.Errorf("expected connected and logged in, got %+v", status)
	}
}

func TestSessionService_RequestPairing(t *testing.T) {
	mockCli := &MockClient{loggedIn: false}
	mockNotif := &MockNotifier{}
	mockStore := &MockStore{}

	svc := service.NewSessionService(mockCli, mockNotif, mockStore, "", "TestBot")

	ctx := context.Background()
	code, err := svc.RequestPairing(ctx, domain.PairingRequest{
		PhoneNumber: "6281234567890",
	})
	if err != nil {
		t.Fatalf("request pairing failed: %v", err)
	}

	if code != "1234-5678" {
		t.Errorf("expected code 1234-5678, got %s", code)
	}

	if len(mockNotif.codes) == 0 || mockNotif.codes[0] != "1234-5678" {
		t.Errorf("expected pairing code sent to notifier")
	}
}

func TestSessionService_Disconnect(t *testing.T) {
	mockCli := &MockClient{connected: true, loggedIn: true}
	mockNotif := &MockNotifier{}
	mockStore := &MockStore{}

	svc := service.NewSessionService(mockCli, mockNotif, mockStore, "6281234567890", "TestBot")

	ctx := context.Background()
	err := svc.Disconnect(ctx)
	if err != nil {
		t.Fatalf("disconnect failed: %v", err)
	}

	if mockCli.IsConnected() {
		t.Errorf("expected client to be disconnected")
	}
}

func TestSessionService_QRCode(t *testing.T) {
	mockCli := &MockClient{loggedIn: false}
	mockNotif := &MockNotifier{}
	mockStore := &MockStore{}

	svc := service.NewSessionService(mockCli, mockNotif, mockStore, "6281234567890", "TestBot")
	svc.SetQRCode("2@mock_qr_code_string,1234,5678")

	qr, err := svc.GetQRCode(context.Background())
	if err != nil {
		t.Fatalf("get qr error: %v", err)
	}
	if qr.IsLoggedIn {
		t.Errorf("expected not logged in")
	}
	if qr.QRCode != "2@mock_qr_code_string,1234,5678" {
		t.Errorf("unexpected qr code: %s", qr.QRCode)
	}
}
