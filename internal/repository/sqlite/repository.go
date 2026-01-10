package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"

	"github.com/lucasew/mise-ci/internal/repository"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Repository struct {
	db      *sql.DB
	queries *Queries
}

func NewRepository(dbPath string) (*Repository, error) {
	// Ensure directory exists
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}

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
	var finishedAt sql.NullTime
	if meta.FinishedAt != nil {
		finishedAt = sql.NullTime{Time: *meta.FinishedAt, Valid: true}
	}

	var exitCode sql.NullInt64
	if meta.ExitCode != nil {
		exitCode = sql.NullInt64{Int64: int64(*meta.ExitCode), Valid: true}
	}

	var repoURL sql.NullString
	if meta.RepoURL != "" {
		repoURL = sql.NullString{String: meta.RepoURL, Valid: true}
	}

	return r.queries.CreateRun(ctx, CreateRunParams{
		ID:            meta.ID,
		Status:        string(meta.Status),
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
		code := int32(row.ExitCode.Int64)
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

	var exitCodeDB sql.NullInt64
	if exitCode != nil {
		exitCodeDB = sql.NullInt64{Int64: int64(*exitCode), Valid: true}
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
		Limit:   limit,
		Offset:  offset,
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
			code := int32(row.ExitCode.Int64)
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

func (r *Repository) UpsertRule(ctx context.Context, id, ruleID, severity, tool string) error {
	return r.queries.UpsertRule(ctx, UpsertRuleParams{
		ID:       id,
		RuleID:   ruleID,
		Severity: severity,
		Tool:     tool,
	})
}

func (r *Repository) CreateFinding(ctx context.Context, runID, ruleRef, message, path string, line int, fingerprint string) error {
	var lineNull sql.NullInt64
	if line > 0 {
		lineNull = sql.NullInt64{Int64: int64(line), Valid: true}
	}
	var fingerprintNull sql.NullString
	if fingerprint != "" {
		fingerprintNull = sql.NullString{String: fingerprint, Valid: true}
	}
	return r.queries.CreateFinding(ctx, CreateFindingParams{
		RunID:       runID,
		RuleRef:     ruleRef,
		Message:     message,
		Path:        path,
		Line:        lineNull,
		Fingerprint: fingerprintNull,
	})
}

func (r *Repository) BatchUpsertRules(ctx context.Context, rules []repository.Rule) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	qtx := r.queries.WithTx(tx)

	for _, rule := range rules {
		if err := qtx.UpsertRule(ctx, UpsertRuleParams{
			ID:       rule.ID,
			RuleID:   rule.RuleID,
			Severity: rule.Severity,
			Tool:     rule.Tool,
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *Repository) BatchCreateFindings(ctx context.Context, findings []repository.Finding) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	qtx := r.queries.WithTx(tx)

	for _, f := range findings {
		var lineNull sql.NullInt64
		if f.Line > 0 {
			lineNull = sql.NullInt64{Int64: int64(f.Line), Valid: true}
		}
		var fingerprintNull sql.NullString
		if f.Fingerprint != "" {
			fingerprintNull = sql.NullString{String: f.Fingerprint, Valid: true}
		}
		if err := qtx.CreateFinding(ctx, CreateFindingParams{
			RunID:       f.RunID,
			RuleRef:     f.RuleRef,
			Message:     f.Message,
			Path:        f.Path,
			Line:        lineNull,
			Fingerprint: fingerprintNull,
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *Repository) ListFindingsForRun(ctx context.Context, runID string) ([]repository.SarifFinding, error) {
	rows, err := r.queries.ListFindingsForRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	findings := make([]repository.SarifFinding, len(rows))
	for i, row := range rows {
		findings[i] = repository.SarifFinding{
			RuleID:      row.RuleID,
			Message:     row.Message,
			Path:        row.Path,
			Line:        int(row.Line.Int64),
			Severity:    row.Severity,
			Tool:        row.Tool,
			Fingerprint: row.Fingerprint.String,
		}
	}
	return findings, nil
}

func (r *Repository) ListFindingsForRepo(ctx context.Context, repoURL string, limit int) ([]repository.SarifFinding, error) {
	rows, err := r.queries.ListFindingsForRepo(ctx, ListFindingsForRepoParams{
		RepoUrl: sql.NullString{String: repoURL, Valid: true},
		Limit:   int64(limit),
	})
	if err != nil {
		return nil, err
	}
	findings := make([]repository.SarifFinding, len(rows))
	for i, row := range rows {
		findings[i] = repository.SarifFinding{
			RuleID:        row.RuleID,
			Message:       row.Message,
			Path:          row.Path,
			Line:          int(row.Line.Int64),
			Severity:      row.Severity,
			Tool:          row.Tool,
			Fingerprint:   row.Fingerprint.String,
			RunID:         row.RunID,
			CommitMessage: row.CommitMessage,
		}
	}
	return findings, nil
}

func (r *Repository) GetRunsWithoutRepoURL(ctx context.Context, limit int) ([]*repository.RunMetadata, error) {
	rows, err := r.queries.GetRunsWithoutRepoURL(ctx, int64(limit))
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
			code := int32(row.ExitCode.Int64)
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
		Limit:     int64(limit),
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
			code := int32(row.ExitCode.Int64)
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

func (r *Repository) GetNextAvailableRun(ctx context.Context) (string, error) {
	id, err := r.queries.GetNextAvailableRun(ctx)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
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
