package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/lucasew/mise-ci/internal/forge"
	"github.com/lucasew/mise-ci/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRepository for testing
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) CreateRepo(ctx context.Context, repo *repository.Repo) error {
	return m.Called(ctx, repo).Error(0)
}
func (m *MockRepository) GetRepo(ctx context.Context, cloneURL string) (*repository.Repo, error) {
	args := m.Called(ctx, cloneURL)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repository.Repo), args.Error(1)
}
func (m *MockRepository) CreateRun(ctx context.Context, meta *repository.RunMetadata) error {
	return m.Called(ctx, meta).Error(0)
}
func (m *MockRepository) GetRun(ctx context.Context, runID string) (*repository.RunMetadata, error) {
	args := m.Called(ctx, runID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repository.RunMetadata), args.Error(1)
}
func (m *MockRepository) UpdateRunStatus(ctx context.Context, runID string, status string, exitCode *int32) error {
	return m.Called(ctx, runID, status, exitCode).Error(0)
}
func (m *MockRepository) ListRuns(ctx context.Context, filter repository.RunFilter) ([]*repository.RunMetadata, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]*repository.RunMetadata), args.Error(1)
}

func (m *MockRepository) ListRepos(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}
func (m *MockRepository) GetRunsWithoutRepoURL(ctx context.Context, limit int) ([]*repository.RunMetadata, error) {
	args := m.Called(ctx, limit)
	return args.Get(0).([]*repository.RunMetadata), args.Error(1)
}
func (m *MockRepository) UpdateRunRepoURL(ctx context.Context, runID string, repoURL string) error {
	return m.Called(ctx, runID, repoURL).Error(0)
}
func (m *MockRepository) GetStuckRuns(ctx context.Context, olderThan time.Time, limit int) ([]*repository.RunMetadata, error) {
	args := m.Called(ctx, olderThan, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*repository.RunMetadata), args.Error(1)
}
func (m *MockRepository) CheckRepoExists(ctx context.Context, cloneURL string) (bool, error) {
	args := m.Called(ctx, cloneURL)
	return args.Bool(0), args.Error(1)
}
func (m *MockRepository) AppendLog(ctx context.Context, runID string, entry repository.LogEntry) error {
	return m.Called(ctx, runID, entry).Error(0)
}
func (m *MockRepository) AppendLogs(ctx context.Context, runID string, entries []repository.LogEntry) error {
	return m.Called(ctx, runID, entries).Error(0)
}
func (m *MockRepository) GetLogs(ctx context.Context, runID string) ([]repository.LogEntry, error) {
	args := m.Called(ctx, runID)
	return args.Get(0).([]repository.LogEntry), args.Error(1)
}
func (m *MockRepository) Close() error {
	return m.Called().Error(0)
}

func (m *MockRepository) UpsertRule(ctx context.Context, id, ruleID, severity, tool string) error {
	return m.Called(ctx, id, ruleID, severity, tool).Error(0)
}
func (m *MockRepository) CreateFinding(ctx context.Context, runID, ruleRef, message, path string, line int) error {
	return m.Called(ctx, runID, ruleRef, message, path, line).Error(0)
}
func (m *MockRepository) BatchUpsertRules(ctx context.Context, rules []repository.Rule) error {
	return m.Called(ctx, rules).Error(0)
}
func (m *MockRepository) BatchCreateFindings(ctx context.Context, findings []repository.Finding) error {
	return m.Called(ctx, findings).Error(0)
}
func (m *MockRepository) ListFindingsForRun(ctx context.Context, runID string) ([]repository.SarifFinding, error) {
	args := m.Called(ctx, runID)
	return args.Get(0).([]repository.SarifFinding), args.Error(1)
}
func (m *MockRepository) ListFindingsForRepo(ctx context.Context, repoURL string, limit int) ([]repository.SarifFinding, error) {
	args := m.Called(ctx, repoURL, limit)
	return args.Get(0).([]repository.SarifFinding), args.Error(1)
}

// MockForge for testing
type MockForge struct {
	mock.Mock
}

func (m *MockForge) ParseWebhook(r *http.Request) (*forge.WebhookEvent, error) {
	args := m.Called(r)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*forge.WebhookEvent), args.Error(1)
}
func (m *MockForge) CloneCredentials(ctx context.Context, repo string) (*forge.Credentials, error) {
	args := m.Called(ctx, repo)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*forge.Credentials), args.Error(1)
}
func (m *MockForge) UpdateStatus(ctx context.Context, repo, sha string, status forge.Status) error {
	return m.Called(ctx, repo, sha, status).Error(0)
}
func (m *MockForge) UploadReleaseAsset(ctx context.Context, repo, tag, name string, data io.Reader) error {
	return m.Called(ctx, repo, tag, name, data).Error(0)
}
func (m *MockForge) CreatePullRequest(ctx context.Context, repo, baseBranch, headBranch, title, body string) (string, error) {
	args := m.Called(ctx, repo, baseBranch, headBranch, title, body)
	return args.String(0), args.Error(1)
}
func (m *MockForge) GetVariables(ctx context.Context, repo string) (map[string]string, error) {
	args := m.Called(ctx, repo)
	return args.Get(0).(map[string]string), args.Error(1)
}
func (m *MockForge) GetCIEnv(event *forge.WebhookEvent) map[string]string {
	args := m.Called(event)
	return args.Get(0).(map[string]string)
}

func TestCleanupStuckRuns(t *testing.T) {
	ctx := context.Background()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("success", func(t *testing.T) {
		repo := new(MockRepository)
		f := new(MockForge)
		c := NewCore(logger, "secret", repo)
		c.SetForge(f)

		runs := []*repository.RunMetadata{
			{
				ID:      "123-abc",
				GitLink: "https://github.com/owner/repo/commit/sha123",
				RepoURL: "https://github.com/owner/repo",
			},
		}

		repo.On("GetStuckRuns", ctx, mock.Anything, 10).Return(runs, nil)

		// Expect Forge update
		f.On("UpdateStatus", ctx, "owner/repo", "sha123", mock.MatchedBy(func(s forge.Status) bool {
			return s.State == forge.StateError && s.Description == "Internal error, please retry"
		})).Return(nil)

		// Expect DB update
		repo.On("UpdateRunStatus", ctx, "123-abc", "error", mock.Anything).Return(nil)
		repo.On("AppendLog", ctx, "123-abc", mock.Anything).Return(nil)

		count, err := c.CleanupStuckRuns(ctx, time.Now(), 10)
		assert.NoError(t, err)
		assert.Equal(t, 1, count)

		repo.AssertExpectations(t)
		f.AssertExpectations(t)
	})

	t.Run("forge update failure", func(t *testing.T) {
		repo := new(MockRepository)
		f := new(MockForge)
		c := NewCore(logger, "secret", repo)
		c.SetForge(f)

		runs := []*repository.RunMetadata{
			{
				ID:      "123-abc",
				GitLink: "https://github.com/owner/repo/commit/sha123",
				RepoURL: "https://github.com/owner/repo",
			},
		}

		repo.On("GetStuckRuns", ctx, mock.Anything, 10).Return(runs, nil)

		// Forge update fails
		f.On("UpdateStatus", ctx, "owner/repo", "sha123", mock.Anything).Return(errors.New("forge error"))

		// Should NOT update DB
		// Assert that UpdateRunStatus is NOT called
        // repo.AssertNotCalled(t, "UpdateRunStatus") -> but we need to verify strict mock

		count, err := c.CleanupStuckRuns(ctx, time.Now(), 10)
		assert.NoError(t, err)
		assert.Equal(t, 0, count)

		repo.AssertExpectations(t)
		f.AssertExpectations(t)
	})
}
