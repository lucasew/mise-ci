package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"

	"github.com/lucasew/mise-ci/internal/core"
	"github.com/lucasew/mise-ci/internal/repository"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Repository struct {
	db      *sql.DB
	queries *Queries
}

func NewRepository(dbPath string) (*Repository, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Configurar SQLite
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Rodar migrations
	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &Repository{
		db:      db,
		queries: New(db),
	}, nil
}

func runMigrations(db *sql.DB) error {
	driver, err := sqlite3.WithInstance(db, &sqlite3.Config{})
	if err != nil {
		return fmt.Errorf("create driver: %w", err)
	}

	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite3", driver)
	if err != nil {
		return fmt.Errorf("create migrate: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}

	return nil
}

func (r *Repository) CreateRun(ctx context.Context, meta *repository.RunMetadata) error {
	return r.queries.CreateRun(ctx, CreateRunParams{
		ID:         meta.ID,
		Status:     string(meta.Status),
		StartedAt:  meta.StartedAt,
		FinishedAt: meta.FinishedAt,
		ExitCode:   meta.ExitCode,
		UiToken:    meta.UIToken,
	})
}

func (r *Repository) GetRun(ctx context.Context, runID string) (*repository.RunMetadata, error) {
	row, err := r.queries.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	return &repository.RunMetadata{
		ID:         row.ID,
		Status:     core.RunStatus(row.Status),
		StartedAt:  row.StartedAt,
		FinishedAt: row.FinishedAt,
		ExitCode:   row.ExitCode,
		UIToken:    row.UiToken,
	}, nil
}

func (r *Repository) UpdateRunStatus(ctx context.Context, runID string, status core.RunStatus, exitCode *int32) error {
	var finishedAt *sql.NullTime
	if status == core.StatusSuccess || status == core.StatusFailure || status == core.StatusError {
		finishedAt = &sql.NullTime{Valid: true}
	}

	return r.queries.UpdateRunStatus(ctx, UpdateRunStatusParams{
		ID:         runID,
		Status:     string(status),
		ExitCode:   exitCode,
		FinishedAt: finishedAt,
	})
}

func (r *Repository) ListRuns(ctx context.Context) ([]*repository.RunMetadata, error) {
	rows, err := r.queries.ListRuns(ctx)
	if err != nil {
		return nil, err
	}

	runs := make([]*repository.RunMetadata, len(rows))
	for i, row := range rows {
		runs[i] = &repository.RunMetadata{
			ID:         row.ID,
			Status:     core.RunStatus(row.Status),
			StartedAt:  row.StartedAt,
			FinishedAt: row.FinishedAt,
			ExitCode:   row.ExitCode,
			UIToken:    row.UiToken,
		}
	}

	return runs, nil
}

func (r *Repository) AppendLog(ctx context.Context, runID string, entry core.LogEntry) error {
	return r.queries.AppendLog(ctx, AppendLogParams{
		RunID:     runID,
		Timestamp: entry.Timestamp,
		Stream:    entry.Stream,
		Data:      entry.Data,
	})
}

func (r *Repository) GetLogs(ctx context.Context, runID string) ([]core.LogEntry, error) {
	rows, err := r.queries.GetLogs(ctx, runID)
	if err != nil {
		return nil, err
	}

	logs := make([]core.LogEntry, len(rows))
	for i, row := range rows {
		logs[i] = core.LogEntry{
			Timestamp: row.Timestamp,
			Stream:    row.Stream,
			Data:      row.Data,
		}
	}

	return logs, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}
