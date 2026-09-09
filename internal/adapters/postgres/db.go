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
// If password authentication fails (e.g. volume had older password), it attempts
// recovery with known candidate passwords and synchronizes the password.
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

		// Check if password authentication failed (SQLSTATE 28P01)
		if isPasswordAuthFailed(pingErr) {
			log.Printf("[DB] Password authentication failed (SQLSTATE 28P01). Attempting automatic credential recovery...")
			_ = db.Close()
			if recoveredDB, recErr := recoverPasswordAuth(ctx, databaseURL); recErr == nil {
				log.Printf("[DB] Database credentials successfully recovered and verified!")
				return recoveredDB, nil
			} else {
				log.Printf("[DB] Automatic credential recovery failed: %v", recErr)
			}
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

func isPasswordAuthFailed(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "28P01" {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "password authentication failed") || strings.Contains(errStr, "28p01")
}

func recoverPasswordAuth(ctx context.Context, targetURL string) (*sql.DB, error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("parse target URL: %w", err)
	}

	targetPass, _ := u.User.Password()
	username := u.User.Username()
	if username == "" {
		username = "postgres"
	}

	// Try candidate passwords that might have been used during prior volume initialization
	candidates := []string{"postgres", "postgres_secure_pass", ""}
	for _, cand := range candidates {
		if cand == targetPass {
			continue // Already tried this
		}

		testURL := *u
		if cand == "" {
			testURL.User = url.User(username)
		} else {
			testURL.User = url.UserPassword(username, cand)
		}

		log.Printf("[DB] Trying recovery password for user '%s'...", username)
		testDB, testErr := sql.Open("pgx", testURL.String())
		if testErr != nil {
			continue
		}

		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		pingErr := testDB.PingContext(pingCtx)
		cancel()

		if pingErr == nil {
			log.Printf("[DB] Connected to PostgreSQL using recovery password! Synchronizing password in Postgres...")
			escapedPass := strings.ReplaceAll(targetPass, "'", "''")
			alterQuery := fmt.Sprintf("ALTER USER \"%s\" WITH PASSWORD '%s';", username, escapedPass)
			if _, execErr := testDB.ExecContext(ctx, alterQuery); execErr != nil {
				log.Printf("[DB] Notice: Could not alter user password (%v), proceeding with working connection.", execErr)
				return testDB, nil
			}
			log.Printf("[DB] PostgreSQL user password successfully synchronized.")
			_ = testDB.Close()

			// Reconnect with target URL
			finalDB, finalErr := sql.Open("pgx", targetURL)
			if finalErr == nil {
				return finalDB, nil
			}
			return testDB, nil
		}
		_ = testDB.Close()
	}

	return nil, fmt.Errorf("all candidate passwords failed")
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
