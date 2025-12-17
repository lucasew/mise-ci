package repository

import (
	"context"
	"time"

	"github.com/lucasew/mise-ci/internal/core"
)

// Repository defines the storage abstraction for run information
type Repository interface {
	// Run lifecycle
	CreateRun(ctx context.Context, meta *RunMetadata) error
	GetRun(ctx context.Context, runID string) (*RunMetadata, error)
	UpdateRunStatus(ctx context.Context, runID string, status core.RunStatus, exitCode *int32) error
	ListRuns(ctx context.Context) ([]*RunMetadata, error)

	// Log operations
	AppendLog(ctx context.Context, runID string, entry core.LogEntry) error
	GetLogs(ctx context.Context, runID string) ([]core.LogEntry, error)

	// Cleanup
	Close() error
}

// RunMetadata represents persistent run information (without logs)
type RunMetadata struct {
	ID         string
	Status     core.RunStatus
	StartedAt  time.Time
	FinishedAt *time.Time
	ExitCode   *int32
	UIToken    string
}
