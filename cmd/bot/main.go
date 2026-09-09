package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/antiban"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/backup"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/discord"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/postgres"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/rest"
	waAdapter "github.com/ipds6104/whatsmeow-coolify-bot/internal/adapters/whatsmeow"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/config"
	"github.com/ipds6104/whatsmeow-coolify-bot/internal/service"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.LoadConfig()

	log.Println("==================================================")
	log.Println("  whatsmeow-coolify-bot - WhatsApp Enterprise Bot  ")
	log.Println("  Architecture: Hexagonal (Ports & Adapters)       ")
	log.Println("  Coolify FQDN:", cfg.CoolifyFQDN)
	log.Println("  Port:", cfg.Port)
	log.Println("==================================================")

	waLogger := waLog.Stdout("Whatsmeow", cfg.LogLevel, true)

	// 1. Database Connection (Postgres) with Retry & Auto-creation
	db, err := postgres.ConnectWithRetry(ctx, cfg.DatabaseURL, 15)
	if err != nil {
		log.Fatalf("Fatal: PostgreSQL connection failed after retries: %v", err)
	}
	defer db.Close()

	// 2. Whatsmeow SqlStore Container & Schema Upgrade
	log.Println("[WHATSMEOW] Initializing SqlStore container...")
	container := sqlstore.NewWithDB(db, "pgx", waLogger)

	log.Println("[WHATSMEOW] Running schema upgrade...")
	if err := container.Upgrade(ctx); err != nil {
		log.Fatalf("Fatal: failed to upgrade whatsmeow schema: %v", err)
	}
	log.Println("[WHATSMEOW] Schema upgrade completed successfully.")

	log.Println("[WHATSMEOW] Retrieving primary device store...")
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		log.Fatalf("Fatal: failed to retrieve device store: %v", err)
	}
	log.Printf("[WHATSMEOW] Device store loaded (hasSession: %v)", device.ID != nil)

	rawClient := whatsmeow.NewClient(device, waLogger)

	// 3. Outbound Driven Adapters
	clientAdapter := waAdapter.NewClientAdapter(rawClient)
	notifier := discord.NewNotifier(cfg.DiscordWebhookURL)
	guard := antiban.NewGuard(cfg.AntibanPreset)
	pgStore := postgres.NewStore(db)

	log.Println("[MIGRATE] Running database chat_messages migration...")
	if err := pgStore.Migrate(ctx); err != nil {
		log.Fatalf("Fatal: database migration failed: %v", err)
	}

	log.Printf("[BACKUP] Initializing backup store at: %s", cfg.BackupDir)
	backupStore, err := backup.NewFileStore(cfg.BackupDir)
	if err != nil {
		log.Fatalf("Fatal: failed to initialize backup file store: %v", err)
	}
	log.Println("[BACKUP] Backup file store ready.")

	// 4. Domain & Application Services
	sessionService := service.NewSessionService(
		clientAdapter,
		notifier,
		pgStore,
		cfg.WAPhoneNumber,
		cfg.WAClientName,
	)

	waService := service.NewWhatsAppService(
		clientAdapter,
		guard,
		pgStore,
	)

	backupService := service.NewBackupService(
		clientAdapter,
		pgStore,
		backupStore,
	)

	schedulerService := service.NewSchedulerService(
		guard,
		backupService,
		notifier,
		sessionService,
		service.SchedulerConfig{
			RetentionDays:   cfg.BackupRetentionDays,
			ScheduledGroups: cfg.BackupScheduledGroups,
			IntervalHours:   cfg.BackupIntervalHours,
		},
	)

	// 5. Inbound Whatsmeow Event Handler
	evtHandler := waAdapter.NewEventHandler(sessionService, notifier, pgStore)
	clientAdapter.AddEventHandler(evtHandler.HandleEvent)

	// 6. Inbound REST API Server
	httpRouter := rest.NewRouter(rest.RouterConfig{
		APIKey:          cfg.APIKey,
		SessionService:  sessionService,
		WhatsAppService: waService,
		BackupService:   backupService,
	})

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpRouter,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("[REST] HTTP REST server listening on port :%s", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Fatal: HTTP server failed: %v", err)
		}
	}()

	// 7. Start Session Service, Supervisor, & Scheduler
	if err := sessionService.Start(ctx); err != nil {
		log.Printf("[WARN] Session service start returned error: %v", err)
	}

	go sessionService.SupervisorLoop(ctx)
	schedulerService.Start(ctx)

	_ = notifier.Notify(ctx, fmt.Sprintf(":rocket: **whatsmeow-coolify-bot** berhasil dijalankan di Coolify!\nPort: `%s` | Anti-ban: `%s`", cfg.Port, cfg.AntibanPreset))

	// 8. Graceful Shutdown on SIGINT / SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	sig := <-sigCh

	log.Printf("Received termination signal (%v). Initiating graceful shutdown...", sig)
	_ = notifier.Notify(ctx, ":wave: Bot sedang restart/redeploy (shutdown terkontrol) — sesi di Postgres aman.")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown warning: %v", err)
	}

	_ = sessionService.Disconnect(shutdownCtx)
	log.Println("Graceful shutdown completed cleanly.")
}
