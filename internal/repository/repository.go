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
	ListRuns(ctx context.Context) ([]*RunMetadata, error)

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
