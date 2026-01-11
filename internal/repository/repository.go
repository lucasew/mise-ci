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
	UpsertRule(ctx context.Context, id, ruleID, severity, tool string) error
	CreateFinding(ctx context.Context, runID, ruleRef, message, path string, line int, fingerprint string) error
	BatchUpsertRules(ctx context.Context, rules []Rule) error
	BatchCreateFindings(ctx context.Context, findings []Finding) error
	ListFindingsForRun(ctx context.Context, runID string) ([]SarifFinding, error)
	ListFindingsForRepo(ctx context.Context, repoURL string, limit int) ([]SarifFinding, error)

	// Maintenance
	GetRunsWithoutRepoURL(ctx context.Context, limit int) ([]*RunMetadata, error)
	UpdateRunRepoURL(ctx context.Context, runID string, repoURL string) error
	GetStuckRuns(ctx context.Context, olderThan time.Time, limit int) ([]*RunMetadata, error)
	CheckRepoExists(ctx context.Context, cloneURL string) (bool, error)

	// Queue operations
	GetNextAvailableRun(ctx context.Context) (string, error)

	// Log operations
	AppendLog(ctx context.Context, runID string, entry LogEntry) error
	AppendLogs(ctx context.Context, runID string, entries []LogEntry) error
	GetLogs(ctx context.Context, runID string) ([]LogEntry, error)

	// Worker Token lifecycle
	CreateWorkerToken(ctx context.Context, token *WorkerToken) error
	GetWorkerToken(ctx context.Context, id string) (*WorkerToken, error)
	ListWorkerTokens(ctx context.Context) ([]*WorkerToken, error)
	RevokeWorkerToken(ctx context.Context, id string, revokedAt time.Time) error
	DeleteWorkerToken(ctx context.Context, id string) error

	// Cleanup
	Close() error
}

// WorkerToken represents a worker authentication token
type WorkerToken struct {
	ID         string
	Name       string
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
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

// SarifFinding represents a single issue finding
type SarifFinding struct {
	RuleID        string
	Message       string
	Path          string
	Line          int
	Severity      string
	Tool          string
	Fingerprint   string
	RunID         string // for repo view
	CommitMessage string // for repo view
}

type Rule struct {
	ID       string
	RuleID   string
	Severity string
	Tool     string
}

type Finding struct {
	RunID       string
	RuleRef     string
	Message     string
	Path        string
	Line        int
	Fingerprint string
}
