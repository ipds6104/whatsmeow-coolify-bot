package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// ConnectWithRetry attempts to connect and ping PostgreSQL with exponential backoff.
// If the target database does not exist (e.g. initial run with pre-existing cluster),
// it automatically connects to the administrative database and creates it.
func ConnectWithRetry(ctx context.Context, databaseURL string, maxAttempts int) (*sql.DB, error) {
	sanitizedURL := SanitizeURL(databaseURL)
	log.Printf("[DB] Target database URL: %s", sanitizedURL)

	var lastErr error
	backoff := 1 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		log.Printf("[DB] Connecting to PostgreSQL (attempt %d/%d)...", attempt, maxAttempts)

		db, err := sql.Open("pgx", databaseURL)
		if err != nil {
			lastErr = fmt.Errorf("failed to open sql driver: %w", err)
			log.Printf("[DB] Driver open error: %v. Retrying in %v...", err, backoff)
			time.Sleep(backoff)
			if backoff < 5*time.Second {
				backoff += 1 * time.Second
			}
			continue
		}

		db.SetMaxOpenConns(20)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(10 * time.Minute)
		db.SetConnMaxIdleTime(5 * time.Minute)

		pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
		pingErr := db.PingContext(pingCtx)
		pingCancel()

		if pingErr == nil {
			log.Printf("[DB] PostgreSQL connection verified and ping successful!")
			return db, nil
		}

		// Check if database does not exist (SQLSTATE 3D000)
		if isDatabaseNotExist(pingErr) {
			log.Printf("[DB] Target database does not exist. Attempting automatic creation...")
			_ = db.Close()
			if createErr := ensureDatabaseExists(ctx, databaseURL); createErr != nil {
				log.Printf("[DB] Failed to auto-create database: %v", createErr)
			} else {
				log.Printf("[DB] Target database created successfully, retrying connection...")
				continue
			}
		}

		lastErr = pingErr
		_ = db.Close()
		log.Printf("[DB] PostgreSQL ping attempt %d failed: %v. Retrying in %v...", attempt, pingErr, backoff)
		time.Sleep(backoff)
		if backoff < 5*time.Second {
			backoff += 1 * time.Second
		}
	}

	return nil, fmt.Errorf("could not connect to PostgreSQL after %d attempts: %w", maxAttempts, lastErr)
}

func isDatabaseNotExist(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "3D000" {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "does not exist") && strings.Contains(errStr, "database")
}

func ensureDatabaseExists(ctx context.Context, targetURL string) error {
	u, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("parse target URL: %w", err)
	}

	targetDB := strings.TrimPrefix(u.Path, "/")
	if targetDB == "" || targetDB == "postgres" {
		return nil
	}

	// Connect to default administrative 'postgres' database
	adminURL := *u
	adminURL.Path = "/postgres"

	adminDB, err := sql.Open("pgx", adminURL.String())
	if err != nil {
		return fmt.Errorf("open admin DB: %w", err)
	}
	defer adminDB.Close()

	adminCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Safe identifier quoting
	safeDBName := strings.ReplaceAll(targetDB, "\"", "\"\"")
	createQuery := fmt.Sprintf("CREATE DATABASE \"%s\"", safeDBName)

	if _, err := adminDB.ExecContext(adminCtx, createQuery); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P04" { // duplicate_database
			return nil
		}
		return fmt.Errorf("execute create database query: %w", err)
	}

	log.Printf("[DB] Database \"%s\" created on PostgreSQL cluster.", targetDB)
	return nil
}

// SanitizeURL masks credentials in a database connection URL for safe logging.
func SanitizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "<unparseable-url>"
	}
	if u.User != nil {
		if _, hasPass := u.User.Password(); hasPass {
			u.User = url.UserPassword(u.User.Username(), "REDACTED")
		}
	}
	return u.String()
}
