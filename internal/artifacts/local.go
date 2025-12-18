package artifacts

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalStorage implements Storage using the local filesystem.
type LocalStorage struct {
	basePath string
}

// NewLocalStorage creates a new LocalStorage instance.
func NewLocalStorage(basePath string) *LocalStorage {
	return &LocalStorage{basePath: basePath}
}

// Save saves an artifact to the local filesystem.
// It creates the directory structure {basePath}/{runID} if it doesn't exist.
func (ls *LocalStorage) Save(ctx context.Context, runID string, name string, data io.Reader) error {
	runDir := filepath.Join(ls.basePath, runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}

	// Sanitize name to prevent directory traversal
	// Use filepath.Base to ensure we only get the filename
	filename := filepath.Base(name)
	filePath := filepath.Join(runDir, filename)

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create artifact file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, data); err != nil {
		return fmt.Errorf("failed to write artifact data: %w", err)
	}

	return nil
}

// Get retrieves an artifact from the local filesystem.
func (ls *LocalStorage) Get(ctx context.Context, runID string, name string) (io.ReadCloser, error) {
	filename := filepath.Base(name)
	filePath := filepath.Join(ls.basePath, runID, filename)

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open artifact file: %w", err)
	}

	return file, nil
}

// List lists all artifacts for a runID.
func (ls *LocalStorage) List(ctx context.Context, runID string) ([]string, error) {
	runDir := filepath.Join(ls.basePath, runID)

	// Check if directory exists
	if _, err := os.Stat(runDir); os.IsNotExist(err) {
		return []string{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to check artifact directory: %w", err)
	}

	entries, err := os.ReadDir(runDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read artifact directory: %w", err)
	}

	var artifacts []string
	for _, entry := range entries {
		if !entry.IsDir() {
			artifacts = append(artifacts, entry.Name())
		}
	}

	return artifacts, nil
}
