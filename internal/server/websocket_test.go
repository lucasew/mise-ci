package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
    "time"

	"github.com/gorilla/websocket"
	"github.com/lucasew/mise-ci/internal/core"
	pb "github.com/lucasew/mise-ci/internal/proto"
	"github.com/lucasew/mise-ci/internal/repository"
	"github.com/lucasew/mise-ci/internal/version"
	"google.golang.org/protobuf/proto"
)

// MockRepository implements repository.Repository for testing
type MockRepository struct{}

func (m *MockRepository) CreateRun(ctx context.Context, meta *repository.RunMetadata) error { return nil }
func (m *MockRepository) GetRun(ctx context.Context, runID string) (*repository.RunMetadata, error) {
	return &repository.RunMetadata{ID: runID, Status: "running", UIToken: "test-token"}, nil
}
func (m *MockRepository) UpdateRunStatus(ctx context.Context, runID string, status string, exitCode *int32) error { return nil }
func (m *MockRepository) ListRuns(ctx context.Context, filter repository.RunFilter) ([]*repository.RunMetadata, error) { return nil, nil }
func (m *MockRepository) ListRepos(ctx context.Context) ([]string, error) { return nil, nil }
func (m *MockRepository) AppendLog(ctx context.Context, runID string, entry repository.LogEntry) error { return nil }
func (m *MockRepository) AppendLogs(ctx context.Context, runID string, entries []repository.LogEntry) error { return nil }
func (m *MockRepository) GetLogs(ctx context.Context, runID string) ([]repository.LogEntry, error) { return nil, nil }
func (m *MockRepository) CreateRepo(ctx context.Context, repo *repository.Repo) error { return nil }
func (m *MockRepository) GetRepo(ctx context.Context, cloneURL string) (*repository.Repo, error) { return nil, nil }
func (m *MockRepository) Close() error { return nil }

// New methods
func (m *MockRepository) GetRunsWithoutRepoURL(ctx context.Context, limit int) ([]*repository.RunMetadata, error) { return nil, nil }
func (m *MockRepository) UpdateRunRepoURL(ctx context.Context, runID string, repoURL string) error { return nil }
func (m *MockRepository) GetStuckRuns(ctx context.Context, olderThan time.Time, limit int) ([]*repository.RunMetadata, error) { return nil, nil }
func (m *MockRepository) CheckRepoExists(ctx context.Context, cloneURL string) (bool, error) { return false, nil }
func (m *MockRepository) UpsertRule(ctx context.Context, id, ruleID, severity, tool string) error { return nil }
func (m *MockRepository) CreateFinding(ctx context.Context, runID, ruleRef, message, path string, line int, fingerprint string) error { return nil }
func (m *MockRepository) BatchUpsertRules(ctx context.Context, rules []repository.Rule) error { return nil }
func (m *MockRepository) BatchCreateFindings(ctx context.Context, findings []repository.Finding) error { return nil }
func (m *MockRepository) ListFindingsForRun(ctx context.Context, runID string) ([]repository.SarifFinding, error) { return nil, nil }
func (m *MockRepository) ListFindingsForRepo(ctx context.Context, repoURL string, limit int) ([]repository.SarifFinding, error) { return nil, nil }

func TestWebSocketHandshake(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	repo := &MockRepository{}
	c := core.NewCore(logger, "test-secret", repo)

	// Create run and set context env
	runID := "test-run"
	_ = c.CreateRun(runID, "", "", "", "", "")

	expectedEnv := map[string]string{
		"TEST_VAR": "test-value",
		"GITHUB_TOKEN": "secret-token",
	}
	c.SetRunEnv(runID, expectedEnv)

	// Get worker token
token, err := c.GenerateWorkerToken(runID)
if err != nil {
	t.Fatalf("failed to generate worker token: %v", err)
}

	// Setup Server
	wsServer := NewWebSocketServer(c, logger)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsServer.HandleConnect)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect
	wsURL := "ws" + server.URL[4:] + "/ws"
	header := http.Header{}
	header.Add("Authorization", "Bearer "+token)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// 1. Send RunnerInfo
	info := &pb.WorkerMessage{
		Payload: &pb.WorkerMessage_RunnerInfo{
			RunnerInfo: &pb.RunnerInfo{
				Hostname: "test-host",
				Os:       "linux",
				Arch:     "amd64",
				Version:  version.Get(),
			},
		},
	}
data, err := proto.Marshal(info)
if err != nil {
	t.Fatalf("failed to marshal RunnerInfo: %v", err)
}
	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("Write RunnerInfo failed: %v", err)
	}

	// 2. Send ContextRequest
	req := &pb.WorkerMessage{
		Payload: &pb.WorkerMessage_ContextRequest{
			ContextRequest: &pb.ContextRequest{},
		},
	}
data, err = proto.Marshal(req)
if err != nil {
	t.Fatalf("failed to marshal ContextRequest: %v", err)
}
	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("Write ContextRequest failed: %v", err)
	}

	// 3. Receive ContextResponse
	_, msgData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Read ContextResponse failed: %v", err)
	}

	var resp pb.ServerMessage
	if err := proto.Unmarshal(msgData, &resp); err != nil {
		t.Fatalf("Unmarshal ContextResponse failed: %v", err)
	}

	ctxResp, ok := resp.Payload.(*pb.ServerMessage_ContextResponse)
	if !ok {
		t.Fatalf("Expected ContextResponse, got %T", resp.Payload)
	}

	if len(ctxResp.ContextResponse.Env) != len(expectedEnv) {
		t.Errorf("Expected %d env vars, got %d", len(expectedEnv), len(ctxResp.ContextResponse.Env))
	}

	if val := ctxResp.ContextResponse.Env["TEST_VAR"]; val != "test-value" {
		t.Errorf("Expected TEST_VAR=test-value, got %s", val)
	}
}
