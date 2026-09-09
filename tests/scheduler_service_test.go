package tests

import (
	"context"
	"testing"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/antiban"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/service"
)

func TestSchedulerService_Lifecycle(t *testing.T) {
	guard := antiban.NewGuard("moderate")
	mockCli := &MockClient{connected: true, loggedIn: true}
	mockStore := &MockStore{}
	mockNotif := &MockNotifier{}

	fileStore := &MockFileStore{}
	backupSvc := service.NewBackupService(mockCli, mockStore, fileStore)
	sessionSvc := service.NewSessionService(mockCli, mockNotif, mockStore, "6281234567890", "TestBot")

	scheduler := service.NewSchedulerService(
		guard,
		backupSvc,
		mockNotif,
		sessionSvc,
		service.SchedulerConfig{
			RetentionDays:   7,
			ScheduledGroups: []string{"12345@g.us"},
			IntervalHours:   1,
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	scheduler.Start(ctx)

	// Allow goroutines to initialize
	time.Sleep(50 * time.Millisecond)

	// Cancel context to ensure graceful background goroutine teardown
	cancel()
	time.Sleep(50 * time.Millisecond)
}

type MockFileStore struct{}

func (m *MockFileStore) SaveBackup(ctx context.Context, result domain.BackupResult, data []byte) error {
	return nil
}

func (m *MockFileStore) ReadBackup(ctx context.Context, backupID string) (domain.BackupResult, []byte, error) {
	return domain.BackupResult{}, []byte("mock"), nil
}

func (m *MockFileStore) ListBackups(ctx context.Context) ([]domain.BackupResult, error) {
	return []domain.BackupResult{}, nil
}

func (m *MockFileStore) PurgeOldBackups(ctx context.Context, retention time.Duration) (int, error) {
	return 2, nil
}
