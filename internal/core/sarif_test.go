package core

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lucasew/mise-ci/internal/artifacts"
	"github.com/lucasew/mise-ci/internal/config"
	"github.com/lucasew/mise-ci/internal/repository"
	"github.com/lucasew/mise-ci/internal/repository/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIngestSARIF(t *testing.T) {
	// 1. Setup in-memory SQLite
	dbPath := filepath.Join(t.TempDir(), "test.db")
	repo, err := sqlite.NewRepository(dbPath)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	core := NewCore(logger, "secret", repo)

	// Create a dummy run and repo
	ctx := context.Background()
	repoURL := "https://github.com/example/repo"
	runID := "run-1"

	err = repo.CreateRepo(ctx, &repository.Repo{CloneURL: repoURL})
	require.NoError(t, err)

	// Use core to create run to ensure everything is linked
	core.CreateRun(runID, repoURL+"/commit/sha", repoURL, "fix: stuff", "me", "main")

	service := NewService(core, nil, nil, &artifacts.LocalStorage{}, &config.Config{}, logger)

	// 2. Create a dummy Go file with a lint error
	workDir := t.TempDir()
	goFile := filepath.Join(workDir, "bad.go")
	err = os.WriteFile(goFile, []byte(`package main
import "fmt"
func main() {
	fmt.Printf("Hello") // Error: printf without formatting verbs
}
`), 0644)
	require.NoError(t, err)

	// 3. Run golangci-lint or mock
	cmd := exec.Command("golangci-lint", "run", "--out-format", "sarif", "--issues-exit-code", "0")
	cmd.Dir = workDir

	// Try adding mise shims to path just in case
	home, _ := os.UserHomeDir()
	newPath := filepath.Join(home, ".local/share/mise/shims") + string(os.PathListSeparator) + os.Getenv("PATH")
	cmd.Env = append(os.Environ(), "PATH="+newPath)

	output, err := cmd.Output()
	if err != nil {
		t.Logf("golangci-lint failed to run (path=%s): %v", newPath, err)

		// Fallback to hardcoded SARIF for robustness
		output = []byte(`{
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "golangci-lint"
        }
      },
      "results": [
        {
          "ruleId": "printf",
          "message": {
            "text": "printf: non-constant format string in call to fmt.Printf"
          },
          "level": "error",
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": {
                  "uri": "bad.go"
                },
                "region": {
                  "startLine": 4
                }
              }
            }
          ]
        }
      ]
    }
  ]
}`)
	} else {
		if len(output) == 0 {
			t.Log("golangci-lint returned empty output")
		}
	}

	// 4. Ingest SARIF
	err = service.IngestSARIF(ctx, runID, output)
	require.NoError(t, err)

	// 5. Verify DB (New Schema)
	issues, err := repo.ListFindingsForRepo(ctx, repoURL, 10)
	require.NoError(t, err)

	assert.NotEmpty(t, issues)
	assert.Equal(t, "golangci-lint", issues[0].Tool)
	assert.Equal(t, "printf", issues[0].RuleID)
	assert.Equal(t, "bad.go", issues[0].Path)
	assert.Equal(t, 4, issues[0].Line)
	assert.Equal(t, runID, issues[0].RunID)
}
