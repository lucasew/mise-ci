package repository

import (
	"context"
	"time"
)

// Repository defines the storage abstraction for run information
type Repository interface {
	// Repo lifecycle
	CreateRepo(ctx context.Context, repo *Repo) error
	GetRepo(ctx context.Context, cloneURL string) (*Repo, error)

	// Run lifecycle
	CreateRun(ctx context.Context, meta *RunMetadata) error
	GetRun(ctx context.Context, runID string) (*RunMetadata, error)
	UpdateRunStatus(ctx context.Context, runID string, status string, exitCode *int32) error
	ListRuns(ctx context.Context, filter RunFilter) ([]*RunMetadata, error)

	// Repo operations
	ListRepos(ctx context.Context) ([]string, error)

	// SARIF operations
	UpsertIssue(ctx context.Context, id, ruleID, message, severity, tool string) error
	CreateOccurrence(ctx context.Context, issueID, runID, path string, line int) error
	ListSarifIssuesForRun(ctx context.Context, runID string) ([]SarifIssue, error)
	ListSarifIssuesForRepo(ctx context.Context, repoURL string, limit int) ([]SarifIssue, error)

	// Maintenance
	GetRunsWithoutRepoURL(ctx context.Context, limit int) ([]*RunMetadata, error)
	UpdateRunRepoURL(ctx context.Context, runID string, repoURL string) error
	GetStuckRuns(ctx context.Context, olderThan time.Time, limit int) ([]*RunMetadata, error)
	CheckRepoExists(ctx context.Context, cloneURL string) (bool, error)

	// Log operations
	AppendLog(ctx context.Context, runID string, entry LogEntry) error
	AppendLogs(ctx context.Context, runID string, entries []LogEntry) error
	GetLogs(ctx context.Context, runID string) ([]LogEntry, error)

	// Cleanup
	Close() error
}

// Repo represents a repository
type Repo struct {
	CloneURL string
}

// RunMetadata represents persistent run information (without logs)
type RunMetadata struct {
	ID            string
	Status        string
	StartedAt     time.Time
	FinishedAt    *time.Time
	ExitCode      *int32
	UIToken       string
	GitLink       string
	RepoURL       string
	CommitMessage string
	Author        string
	Branch        string
}

// LogEntry represents a single log line
type LogEntry struct {
	Timestamp time.Time
	Stream    string // "stdout", "stderr", "system"
	Data      string
}

// SarifIssue represents a single issue found by a linter
type SarifIssue struct {
	RuleID        string
	Message       string
	Path          string
	Line          int
	Severity      string
	Tool          string
	RunID         string // for repo view
	CommitMessage string // for repo view
}
