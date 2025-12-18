package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"

	"github.com/lucasew/mise-ci/internal/repository"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Repository struct {
	db      *sql.DB
	queries *Queries
}

func NewRepository(dsn string) (*Repository, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

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
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("create driver: %w", err)
	}

	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrate: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}

	return nil
}

func (r *Repository) CreateRun(ctx context.Context, meta *repository.RunMetadata) error {
	var finishedAt sql.NullTime
	if meta.FinishedAt != nil {
		finishedAt = sql.NullTime{Time: *meta.FinishedAt, Valid: true}
	}

	var exitCode sql.NullInt32
	if meta.ExitCode != nil {
		exitCode = sql.NullInt32{Int32: *meta.ExitCode, Valid: true}
	}

	return r.queries.CreateRun(ctx, CreateRunParams{
		ID:         meta.ID,
		Status:     meta.Status,
		StartedAt:  meta.StartedAt,
		FinishedAt: finishedAt,
		ExitCode:   exitCode,
		UiToken:    meta.UIToken,
	})
}

func (r *Repository) GetRun(ctx context.Context, runID string) (*repository.RunMetadata, error) {
	row, err := r.queries.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	var finishedAt *time.Time
	if row.FinishedAt.Valid {
		finishedAt = &row.FinishedAt.Time
	}

	var exitCode *int32
	if row.ExitCode.Valid {
		code := row.ExitCode.Int32
		exitCode = &code
	}

	return &repository.RunMetadata{
		ID:         row.ID,
		Status:     row.Status,
		StartedAt:  row.StartedAt,
		FinishedAt: finishedAt,
		ExitCode:   exitCode,
		UIToken:    row.UiToken,
	}, nil
}

func (r *Repository) UpdateRunStatus(ctx context.Context, runID string, status string, exitCode *int32) error {
	var finishedAt sql.NullTime
	// Finished states: success, failure, error
	if status == "success" || status == "failure" || status == "error" {
		finishedAt = sql.NullTime{Time: time.Now(), Valid: true}
	}

	var exitCodeDB sql.NullInt32
	if exitCode != nil {
		exitCodeDB = sql.NullInt32{Int32: *exitCode, Valid: true}
	}

	return r.queries.UpdateRunStatus(ctx, UpdateRunStatusParams{
		ID:         runID,
		Status:     status,
		ExitCode:   exitCodeDB,
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
		var finishedAt *time.Time
		if row.FinishedAt.Valid {
			finishedAt = &row.FinishedAt.Time
		}

		var exitCode *int32
		if row.ExitCode.Valid {
			code := row.ExitCode.Int32
			exitCode = &code
		}

		runs[i] = &repository.RunMetadata{
			ID:         row.ID,
			Status:     row.Status,
			StartedAt:  row.StartedAt,
			FinishedAt: finishedAt,
			ExitCode:   exitCode,
			UIToken:    row.UiToken,
		}
	}

	return runs, nil
}

func (r *Repository) AppendLog(ctx context.Context, runID string, entry repository.LogEntry) error {
	return r.queries.AppendLog(ctx, AppendLogParams{
		RunID:     runID,
		Timestamp: entry.Timestamp,
		Stream:    entry.Stream,
		Data:      entry.Data,
	})
}

func (r *Repository) GetLogs(ctx context.Context, runID string) ([]repository.LogEntry, error) {
	rows, err := r.queries.GetLogs(ctx, runID)
	if err != nil {
		return nil, err
	}

	logs := make([]repository.LogEntry, len(rows))
	for i, row := range rows {
		logs[i] = repository.LogEntry{
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
