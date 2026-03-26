package global

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/db/dbopen"
	"github.com/pressly/goose/v3"
)

// IndexPath returns the absolute path to the global index database.
func IndexPath() string {
	return filepath.Join(filepath.Dir(config.GlobalConfigData()), "index.db")
}

// Connect opens the global index database, creating the directory and
// running migrations if needed. Returns (*sql.DB, isNewDB, error).
// isNewDB is true when the DB file did not exist before this call.
func Connect(ctx context.Context) (*sql.DB, bool, error) {
	dbPath := IndexPath()

	_, err := os.Stat(dbPath)
	isNew := errors.Is(err, os.ErrNotExist)

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, false, fmt.Errorf("creating index directory: %w", err)
	}

	db, err := dbopen.Open(dbPath)
	if err != nil {
		return nil, false, fmt.Errorf("opening global index: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, false, fmt.Errorf("pinging global index: %w", err)
	}

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		db.Close()
		return nil, false, fmt.Errorf("setting dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		slog.Error("Failed to apply global index migrations", "error", err)
		db.Close()
		return nil, false, fmt.Errorf("applying migrations: %w", err)
	}

	return db, isNew, nil
}
