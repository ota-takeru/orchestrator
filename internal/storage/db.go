package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	sql      *sql.DB
	dataRoot string
}

func Open(ctx context.Context, path string) (*DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("database path is required")
	}
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	handle.SetMaxOpenConns(1)
	handle.SetMaxIdleConns(1)
	db := &DB{sql: handle, dataRoot: filepath.Dir(path)}
	if err := db.configure(ctx); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

func (db *DB) SQL() *sql.DB {
	return db.sql
}

func (db *DB) DataRoot() string {
	return db.dataRoot
}

func (db *DB) configure(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	}
	for _, pragma := range pragmas {
		if _, err := db.sql.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("configure sqlite %q: %w", pragma, err)
		}
	}
	return nil
}

func (db *DB) Migrate(ctx context.Context, migrations []Migration) error {
	if err := ValidateMigrations(migrations); err != nil {
		return err
	}
	applied, err := db.AppliedMigrations(ctx)
	if err != nil {
		return err
	}
	pending, err := PlanMigrations(migrations, applied)
	if err != nil {
		return err
	}
	for _, migration := range pending {
		if err := db.applyMigration(ctx, migration); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) AppliedMigrations(ctx context.Context) ([]AppliedMigration, error) {
	rows, err := db.sql.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		if isNoSuchTable(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()

	var applied []AppliedMigration
	for rows.Next() {
		var migration AppliedMigration
		if err := rows.Scan(&migration.Version, &migration.Checksum); err != nil {
			return nil, err
		}
		applied = append(applied, migration)
	}
	return applied, rows.Err()
}

func (db *DB) applyMigration(ctx context.Context, migration Migration) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
		return fmt.Errorf("apply migration %03d %s: %w", migration.Version, migration.Name, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES (?, ?, ?, ?)",
		migration.Version, migration.Name, migration.Checksum, now,
	); err != nil {
		return fmt.Errorf("record migration %03d %s: %w", migration.Version, migration.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func isNoSuchTable(err error) bool {
	if err == nil {
		return false
	}
	for unwrapped := err; unwrapped != nil; unwrapped = errors.Unwrap(unwrapped) {
		if strings.Contains(unwrapped.Error(), "no such table: schema_migrations") {
			return true
		}
	}
	return strings.Contains(err.Error(), "no such table: schema_migrations")
}
