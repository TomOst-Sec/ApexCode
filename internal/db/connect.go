package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
)

// Connect opens a SQLite database connection and runs migrations.
// It configures the connection pool for optimal SQLite performance.
func Connect(ctx context.Context, dataDir string) (*sql.DB, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("data.dir is not set")
	}
	dbPath := filepath.Join(dataDir, "apexcode.db")

	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}

	// Configure connection pool for SQLite.
	// SQLite works best with a single writer connection to avoid lock contention.
	// We allow more readers but limit total connections to avoid resource waste.
	db.SetMaxOpenConns(1)              // Single writer prevents "database is locked" errors
	db.SetMaxIdleConns(1)              // Keep the connection warm
	db.SetConnMaxLifetime(time.Hour)   // Recycle connections periodically
	db.SetConnMaxIdleTime(time.Minute) // Close idle connections after 1 minute

	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	goose.SetBaseFS(FS)

	if err := goose.SetDialect("sqlite3"); err != nil {
		slog.Error("Failed to set dialect", "error", err)
		return nil, fmt.Errorf("failed to set dialect: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		slog.Error("Failed to apply migrations", "error", err)
		return nil, fmt.Errorf("failed to apply migrations: %w", err)
	}

	return db, nil
}
