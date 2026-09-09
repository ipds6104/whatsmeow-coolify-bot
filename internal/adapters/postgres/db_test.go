package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSanitizeURL(t *testing.T) {
	raw := "postgres://admin:supersecret@db.local:5432/my_database?sslmode=disable"
	sanitized := SanitizeURL(raw)
	if sanitized != "postgres://admin:REDACTED@db.local:5432/my_database?sslmode=disable" {
		t.Fatalf("unexpected sanitized URL: %s", sanitized)
	}

	noPass := "postgres://user@localhost:5432/db"
	if SanitizeURL(noPass) != "postgres://user@localhost:5432/db" {
		t.Fatalf("unexpected sanitized URL for no-pass: %s", SanitizeURL(noPass))
	}

	invalid := "::invalid-url"
	if SanitizeURL(invalid) != "<unparseable-url>" {
		t.Fatalf("unexpected result for invalid URL: %s", SanitizeURL(invalid))
	}
}

func TestIsDatabaseNotExist(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "3D000", Message: "database does not exist"}
	if !isDatabaseNotExist(pgErr) {
		t.Fatalf("expected true for pgconn 3D000 error")
	}

	genericErr := errors.New("FATAL: database \"whatsmeow\" does not exist (SQLSTATE 3D000)")
	if !isDatabaseNotExist(genericErr) {
		t.Fatalf("expected true for generic database does not exist string")
	}

	otherErr := errors.New("connection refused")
	if isDatabaseNotExist(otherErr) {
		t.Fatalf("expected false for connection refused error")
	}

	if isDatabaseNotExist(nil) {
		t.Fatalf("expected false for nil error")
	}
}
