package artifacts

import (
	"context"
	"io"
)

// Storage defines the interface for artifact storage operations.
type Storage interface {
	// Save saves an artifact for a given runID
	Save(ctx context.Context, runID string, name string, data io.Reader) error

	// Get retrieves an artifact (for future use)
	Get(ctx context.Context, runID string, name string) (io.ReadCloser, error)

	// List lists all artifacts for a runID (for future use)
	List(ctx context.Context, runID string) ([]string, error)
}
