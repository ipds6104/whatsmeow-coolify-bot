package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/domain"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/ports"
)

type SchedulerConfig struct {
	RetentionDays   int
	ScheduledGroups []string
	IntervalHours   int
}

// SchedulerService orchestrates autonomous background maintenance, anti-ban hygiene, and scheduled backups.
type SchedulerService struct {
	guard          ports.AntiBanGuardPort
	backupService  ports.BackupService
	notifier       ports.NotifierPort
	sessionService ports.SessionService
	cfg            SchedulerConfig
}

func NewSchedulerService(
	guard ports.AntiBanGuardPort,
	backupService ports.BackupService,
	notifier ports.NotifierPort,
	sessionService ports.SessionService,
	cfg SchedulerConfig,
) *SchedulerService {
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 30
	}
	if cfg.IntervalHours <= 0 {
		cfg.IntervalHours = 24
	}
	return &SchedulerService{
		guard:          guard,
		backupService:  backupService,
		notifier:       notifier,
		sessionService: sessionService,
		cfg:            cfg,
	}
}

// Start begins background maintenance loops and schedules.
func (s *SchedulerService) Start(ctx context.Context) {
	log.Println("[SCHEDULER] Starting background maintenance engine...")
	go s.runAntiBanPruner(ctx)
	go s.runDailyMidnightReset(ctx)
	go s.runBackupRetentionPurge(ctx)
	if len(s.cfg.ScheduledGroups) > 0 {
		go s.runScheduledGroupBackup(ctx)
	}
}

// runAntiBanPruner evicts stale recipient records every 15 minutes to prevent memory leaks.
func (s *SchedulerService) runAntiBanPruner(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruned := s.guard.PruneStale(1 * time.Hour)
			if pruned > 0 {
				log.Printf("[SCHEDULER] Anti-ban hygiene: evicted %d stale recipient records from memory", pruned)
			}
		}
	}
}

// runDailyMidnightReset ensures daily warmup limits are reset deterministically at 00:00 UTC/local.
func (s *SchedulerService) runDailyMidnightReset(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	lastDay := time.Now().YearDay()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentDay := time.Now().YearDay()
			if currentDay != lastDay {
				lastDay = currentDay
				s.guard.ResetDaily()
				stats := s.guard.GetWarmupStats()
				log.Printf("[SCHEDULER] New day reached (%d): anti-ban daily counter reset to 0 (cap: %v, preset: %v)",
					currentDay, stats["daily_cap"], stats["preset"])
			}
		}
	}
}

// runBackupRetentionPurge purges backup archives exceeding configured retention days every 12 hours.
func (s *SchedulerService) runBackupRetentionPurge(ctx context.Context) {
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()

	retention := time.Duration(s.cfg.RetentionDays) * 24 * time.Hour

	// Initial check on startup
	if purged, err := s.backupService.PurgeOldBackups(ctx, retention); err == nil && purged > 0 {
		log.Printf("[SCHEDULER] Housekeeping: purged %d expired backup files exceeding %d days", purged, s.cfg.RetentionDays)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purged, err := s.backupService.PurgeOldBackups(ctx, retention)
			if err != nil {
				log.Printf("[SCHEDULER] Backup retention purge warning: %v", err)
			} else if purged > 0 {
				log.Printf("[SCHEDULER] Housekeeping: purged %d expired backup files exceeding %d days", purged, s.cfg.RetentionDays)
			}
		}
	}
}

// runScheduledGroupBackup executes periodic archives for configured priority groups.
func (s *SchedulerService) runScheduledGroupBackup(ctx context.Context) {
	interval := time.Duration(s.cfg.IntervalHours) * time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("[SCHEDULER] Scheduled group backup active for %d groups (interval: %s)", len(s.cfg.ScheduledGroups), interval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status, err := s.sessionService.GetStatus(ctx)
			if err != nil || !status.IsConnected || !status.IsLoggedIn {
				log.Println("[SCHEDULER] Skipping scheduled group backup: client not logged in or disconnected")
				continue
			}

			for _, groupJID := range s.cfg.ScheduledGroups {
				result, err := s.backupService.BackupGroupChat(ctx, domain.BackupRequest{
					ChatJID:      groupJID,
					IncludeMedia: true,
					Limit:        1000,
					Format:       "json",
				})
				if err != nil {
					log.Printf("[SCHEDULER] Error backing up scheduled group %s: %v", groupJID, err)
					continue
				}

				msg := fmt.Sprintf(":floppy_disk: **Automated Group Backup Completed**\nGroup: `%s` (%s)\nTotal Messages: `%d` | Total Media: `%d` | Size: `%d bytes`",
					result.ChatName, result.ChatJID, result.TotalMessages, result.TotalMedia, result.FileSize)
				_ = s.notifier.Notify(ctx, msg)
				log.Printf("[SCHEDULER] Group backup completed for %s (%d messages)", groupJID, result.TotalMessages)
			}
		}
	}
}
