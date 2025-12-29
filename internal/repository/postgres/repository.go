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

	var repoURL sql.NullString
	if meta.RepoURL != "" {
		repoURL = sql.NullString{String: meta.RepoURL, Valid: true}
	}

	return r.queries.CreateRun(ctx, CreateRunParams{
		ID:            meta.ID,
		Status:        meta.Status,
		StartedAt:     meta.StartedAt,
		FinishedAt:    finishedAt,
		ExitCode:      exitCode,
		UiToken:       meta.UIToken,
		GitLink:       meta.GitLink,
		RepoUrl:       repoURL,
		CommitMessage: meta.CommitMessage,
		Author:        meta.Author,
		Branch:        meta.Branch,
	})
}

func (r *Repository) CreateRepo(ctx context.Context, repo *repository.Repo) error {
	return r.queries.CreateRepo(ctx, repo.CloneURL)
}

func (r *Repository) GetRepo(ctx context.Context, cloneURL string) (*repository.Repo, error) {
	row, err := r.queries.GetRepo(ctx, cloneURL)
	if err != nil {
		return nil, err
	}
	return &repository.Repo{
		CloneURL: row,
	}, nil
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

	var repoURL string
	if row.RepoUrl.Valid {
		repoURL = row.RepoUrl.String
	}

	return &repository.RunMetadata{
		ID:            row.ID,
		Status:        row.Status,
		StartedAt:     row.StartedAt,
		FinishedAt:    finishedAt,
		ExitCode:      exitCode,
		UIToken:       row.UiToken,
		GitLink:       row.GitLink,
		RepoURL:       repoURL,
		CommitMessage: row.CommitMessage,
		Author:        row.Author,
		Branch:        row.Branch,
	}, nil
}

func (r *Repository) UpdateRunStatus(ctx context.Context, runID string, status string, exitCode *int32) error {
	var finishedAt sql.NullTime
	// Finished states: success, failure, error, skipped
	if status == "success" || status == "failure" || status == "error" || status == "skipped" {
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

func (r *Repository) ListRuns(ctx context.Context, filter repository.RunFilter) ([]*repository.RunMetadata, error) {
	limit := int64(filter.Limit)
	if limit <= 0 {
		limit = 100
	}
	offset := int64(filter.Offset)
	if offset < 0 {
		offset = 0
	}
	var repoUrl sql.NullString
	if filter.RepoURL != nil {
		repoUrl = sql.NullString{String: *filter.RepoURL, Valid: true}
	}

	rows, err := r.queries.ListRuns(ctx, ListRunsParams{
		RepoUrl: repoUrl,
		Limit:   int32(limit),
		Offset:  int32(offset),
	})
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

		var repoURL string
		if row.RepoUrl.Valid {
			repoURL = row.RepoUrl.String
		}

		runs[i] = &repository.RunMetadata{
			ID:            row.ID,
			Status:        row.Status,
			StartedAt:     row.StartedAt,
			FinishedAt:    finishedAt,
			ExitCode:      exitCode,
			UIToken:       row.UiToken,
			GitLink:       row.GitLink,
			RepoURL:       repoURL,
			CommitMessage: row.CommitMessage,
			Author:        row.Author,
			Branch:        row.Branch,
		}
	}

	return runs, nil
}

func (r *Repository) ListRepos(ctx context.Context) ([]string, error) {
	rows, err := r.queries.ListRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list repos: %w", err)
	}

	repos := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Valid && row.String != "" {
			repos = append(repos, row.String)
		}
	}
	return repos, nil
}

func (r *Repository) UpsertIssue(ctx context.Context, id, ruleID, message, severity, tool string) error {
	return r.queries.UpsertIssue(ctx, UpsertIssueParams{
		ID:       id,
		RuleID:   ruleID,
		Message:  message,
		Severity: severity,
		Tool:     tool,
	})
}

func (r *Repository) CreateOccurrence(ctx context.Context, issueID, runID, path string, line int) error {
	var lineNull sql.NullInt32
	if line > 0 {
		lineNull = sql.NullInt32{Int32: int32(line), Valid: true}
	}
	return r.queries.CreateOccurrence(ctx, CreateOccurrenceParams{
		IssueID: issueID,
		RunID:   runID,
		Path:    path,
		Line:    lineNull,
	})
}

func (r *Repository) ListSarifIssuesForRun(ctx context.Context, runID string) ([]repository.SarifIssue, error) {
	rows, err := r.queries.ListSarifIssuesForRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	issues := make([]repository.SarifIssue, len(rows))
	for i, row := range rows {
		issues[i] = repository.SarifIssue{
			RuleID:   row.RuleID,
			Message:  row.Message,
			Path:     row.Path,
			Line:     int(row.Line.Int32),
			Severity: row.Severity,
			Tool:     row.Tool,
		}
	}
	return issues, nil
}

func (r *Repository) ListSarifIssuesForRepo(ctx context.Context, repoURL string, limit int) ([]repository.SarifIssue, error) {
	rows, err := r.queries.ListSarifIssuesForRepo(ctx, ListSarifIssuesForRepoParams{
		RepoUrl: sql.NullString{String: repoURL, Valid: true},
		Limit:   int32(limit),
	})
	if err != nil {
		return nil, err
	}
	issues := make([]repository.SarifIssue, len(rows))
	for i, row := range rows {
		issues[i] = repository.SarifIssue{
			RuleID:        row.RuleID,
			Message:       row.Message,
			Path:          row.Path,
			Line:          int(row.Line.Int32),
			Severity:      row.Severity,
			Tool:          row.Tool,
			RunID:         row.RunID,
			CommitMessage: row.CommitMessage,
		}
	}
	return issues, nil
}

func (r *Repository) GetRunsWithoutRepoURL(ctx context.Context, limit int) ([]*repository.RunMetadata, error) {
	rows, err := r.queries.GetRunsWithoutRepoURL(ctx, int32(limit))
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

		var repoURL string
		if row.RepoUrl.Valid {
			repoURL = row.RepoUrl.String
		}

		runs[i] = &repository.RunMetadata{
			ID:            row.ID,
			Status:        row.Status,
			StartedAt:     row.StartedAt,
			FinishedAt:    finishedAt,
			ExitCode:      exitCode,
			UIToken:       row.UiToken,
			GitLink:       row.GitLink,
			RepoURL:       repoURL,
			CommitMessage: row.CommitMessage,
			Author:        row.Author,
			Branch:        row.Branch,
		}
	}
	return runs, nil
}

func (r *Repository) UpdateRunRepoURL(ctx context.Context, runID string, repoURL string) error {
	return r.queries.UpdateRunRepoURL(ctx, UpdateRunRepoURLParams{
		RepoUrl: sql.NullString{String: repoURL, Valid: true},
		ID:      runID,
	})
}

func (r *Repository) GetStuckRuns(ctx context.Context, olderThan time.Time, limit int) ([]*repository.RunMetadata, error) {
	rows, err := r.queries.GetStuckRuns(ctx, GetStuckRunsParams{
		StartedAt: olderThan,
		Limit:     int32(limit),
	})
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

		var repoURL string
		if row.RepoUrl.Valid {
			repoURL = row.RepoUrl.String
		}

		runs[i] = &repository.RunMetadata{
			ID:            row.ID,
			Status:        row.Status,
			StartedAt:     row.StartedAt,
			FinishedAt:    finishedAt,
			ExitCode:      exitCode,
			UIToken:       row.UiToken,
			GitLink:       row.GitLink,
			RepoURL:       repoURL,
			CommitMessage: row.CommitMessage,
			Author:        row.Author,
			Branch:        row.Branch,
		}
	}
	return runs, nil
}

func (r *Repository) CheckRepoExists(ctx context.Context, cloneURL string) (bool, error) {
	_, err := r.queries.CheckRepoExists(ctx, cloneURL)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *Repository) AppendLog(ctx context.Context, runID string, entry repository.LogEntry) error {
	return r.queries.AppendLog(ctx, AppendLogParams{
		RunID:     runID,
		Timestamp: entry.Timestamp,
		Stream:    entry.Stream,
		Data:      entry.Data,
	})
}

func (r *Repository) AppendLogs(ctx context.Context, runID string, entries []repository.LogEntry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	qtx := r.queries.WithTx(tx)

	for _, entry := range entries {
		if err := qtx.AppendLog(ctx, AppendLogParams{
			RunID:     runID,
			Timestamp: entry.Timestamp,
			Stream:    entry.Stream,
			Data:      entry.Data,
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
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
