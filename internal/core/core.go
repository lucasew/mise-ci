package core

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	pb "github.com/lucasew/mise-ci/internal/proto"
	"github.com/lucasew/mise-ci/internal/repository"
)

type RunStatus string

const (
	StatusScheduled RunStatus = "scheduled"
	StatusRunning   RunStatus = "running"
	StatusSuccess   RunStatus = "success"
	StatusFailure   RunStatus = "failure"
	StatusError     RunStatus = "error"
	StatusSkipped   RunStatus = "skipped"
)

type TokenType string

const (
	TokenTypeWorker TokenType = "worker"
	TokenTypeUI     TokenType = "ui"
)

type LogEntry struct {
	Timestamp time.Time
	Stream    string // "stdout", "stderr", "system"
	Data      string
}

type RunInfo struct {
	ID         string
	Status     RunStatus
	StartedAt  time.Time
	FinishedAt *time.Time
	Logs       []LogEntry
	ExitCode   *int32
	UIToken    string
	GitLink    string
}

type Core struct {
	runs           map[string]*Run
	repo           repository.Repository
	mu             sync.RWMutex
	logger         *slog.Logger
	jwtSecret      []byte
	logListener    map[string]*ListenerManager[LogEntry] // run_id -> log listeners manager
	statusListener *ListenerManager[RunInfo]             // global status change listeners manager
}

type Run struct {
	ID          string
	CommandCh   chan *pb.ServerMessage // Commands to send to worker
	ResultCh    chan *pb.WorkerMessage // Results from worker (for logic to consume)
	DoneCh      chan struct{}
	ConnectedCh chan struct{} // Signals when worker connects
}

func NewCore(logger *slog.Logger, secret string, repo repository.Repository) *Core {
	return &Core{
		runs:           make(map[string]*Run),
		repo:           repo,
		logger:         logger,
		jwtSecret:      []byte(secret),
		logListener:    make(map[string]*ListenerManager[LogEntry]),
		statusListener: NewListenerManager[RunInfo](),
	}
}

func (c *Core) CreateRun(id string, gitLink string) *Run {
	c.mu.Lock()
	defer c.mu.Unlock()

	run := &Run{
		ID:          id,
		CommandCh:   make(chan *pb.ServerMessage, 10),
		ResultCh:    make(chan *pb.WorkerMessage, 10),
		DoneCh:      make(chan struct{}),
		ConnectedCh: make(chan struct{}),
	}
	c.runs[id] = run

	uiToken, err := c.generateToken(id, TokenTypeUI)
	if err != nil {
		c.logger.Error("failed to generate UI token", "error", err, "run_id", id)
		// We continue without a token, but this shouldn't happen
	}

	// Save to database
	metadata := &repository.RunMetadata{
		ID:        id,
		Status:    string(StatusScheduled),
		StartedAt: time.Now(),
		UIToken:   uiToken,
		GitLink:   gitLink,
	}

	ctx := context.Background()
	if err := c.repo.CreateRun(ctx, metadata); err != nil {
		c.logger.Error("failed to create run in database", "error", err, "run_id", id)
	}

	// Broadcast to status listeners
	c.statusListener.Broadcast(RunInfo{
		ID:        id,
		Status:    StatusScheduled,
		StartedAt: metadata.StartedAt,
		UIToken:   uiToken,
		GitLink:   gitLink,
	})

	return run
}

func (c *Core) GetRun(id string) (*Run, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	run, ok := c.runs[id]
	return run, ok
}

func (c *Core) ValidateToken(tokenString string) (string, TokenType, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return c.jwtSecret, nil
	})

	if err != nil {
		return "", "", err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		runID, ok := claims["run_id"].(string)
		if !ok {
			return "", "", fmt.Errorf("run_id claim missing or invalid")
		}

		tokenTypeStr, ok := claims["type"].(string)
		var tokenType TokenType
		if !ok {
			// Backwards compatibility: assume worker token if type is missing
			tokenType = TokenTypeWorker
		} else {
			tokenType = TokenType(tokenTypeStr)
		}

		return runID, tokenType, nil
	}

	return "", "", fmt.Errorf("invalid token")
}

func (c *Core) GenerateWorkerToken(runID string) (string, error) {
	return c.generateToken(runID, TokenTypeWorker)
}

func (c *Core) GenerateUIToken(runID string) (string, error) {
	return c.generateToken(runID, TokenTypeUI)
}

func (c *Core) generateToken(runID string, tokenType TokenType) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"run_id": runID,
		"type":   tokenType,
	})
	return token.SignedString(c.jwtSecret)
}

func (c *Core) GetRunUIURL(runID string, baseURL string) string {
	ctx := context.Background()
	meta, err := c.repo.GetRun(ctx, runID)
	if err != nil || meta.UIToken == "" {
		return ""
	}

	// Remove trailing slash if present
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return fmt.Sprintf("%s/ui/run/%s?token=%s", baseURL, runID, meta.UIToken)
}

func (c *Core) AddLog(runID string, stream string, data string) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Stream:    stream,
		Data:      data,
	}

	// Save to database (sync - não pode perder logs!)
	ctx := context.Background()
	repoEntry := repository.LogEntry{
		Timestamp: entry.Timestamp,
		Stream:    entry.Stream,
		Data:      entry.Data,
	}
	if err := c.repo.AppendLog(ctx, runID, repoEntry); err != nil {
		c.logger.Error("failed to persist log", "error", err, "run_id", runID)
	}

	// Broadcast to listeners (real-time streaming)
	c.mu.RLock()
	lm, exists := c.logListener[runID]
	c.mu.RUnlock()

	if exists {
		lm.Broadcast(entry)
	}
}

func (c *Core) UpdateStatus(runID string, status RunStatus, exitCode *int32) {
	// Update in database
	ctx := context.Background()
	if err := c.repo.UpdateRunStatus(ctx, runID, string(status), exitCode); err != nil {
		c.logger.Error("failed to update status in database", "error", err, "run_id", runID)
		return
	}

	// Get updated run info for broadcast
	meta, err := c.repo.GetRun(ctx, runID)
	if err != nil {
		c.logger.Error("failed to get run after status update", "error", err, "run_id", runID)
		return
	}

	// Broadcast status change to listeners
	c.statusListener.Broadcast(RunInfo{
		ID:         meta.ID,
		Status:     RunStatus(meta.Status),
		StartedAt:  meta.StartedAt,
		FinishedAt: meta.FinishedAt,
		ExitCode:   meta.ExitCode,
		UIToken:    meta.UIToken,
	})
}

func (c *Core) GetRunInfo(runID string) (*RunInfo, bool) {
	ctx := context.Background()

	meta, err := c.repo.GetRun(ctx, runID)
	if err != nil {
		return nil, false
	}

	return &RunInfo{
		ID:         meta.ID,
		Status:     RunStatus(meta.Status),
		StartedAt:  meta.StartedAt,
		FinishedAt: meta.FinishedAt,
		ExitCode:   meta.ExitCode,
		UIToken:    meta.UIToken,
		GitLink:    meta.GitLink,
		Logs:       nil, // Logs fetched separately via GetLogsFromRepository
	}, true
}

func (c *Core) GetAllRuns() []RunInfo {
	ctx := context.Background()

	repoRuns, err := c.repo.ListRuns(ctx)
	if err != nil {
		c.logger.Error("failed to list runs from repository", "error", err)
		return []RunInfo{}
	}

	runs := make([]RunInfo, len(repoRuns))
	for i, repoRun := range repoRuns {
		runs[i] = RunInfo{
			ID:         repoRun.ID,
			Status:     RunStatus(repoRun.Status),
			StartedAt:  repoRun.StartedAt,
			FinishedAt: repoRun.FinishedAt,
			Logs:       nil,
			ExitCode:   repoRun.ExitCode,
			UIToken:    repoRun.UIToken,
			GitLink:    repoRun.GitLink,
		}
	}

	return runs
}

func (c *Core) SubscribeLogs(runID string) chan LogEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.logListener[runID]; !ok {
		c.logListener[runID] = NewListenerManager[LogEntry]()
	}
	return c.logListener[runID].Subscribe()
}

func (c *Core) UnsubscribeLogs(runID string, ch chan LogEntry) {
	c.mu.RLock()
	lm, ok := c.logListener[runID]
	c.mu.RUnlock()

	if ok {
		lm.Unsubscribe(ch)
	}
}

func (c *Core) SubscribeStatus() chan RunInfo {
	return c.statusListener.Subscribe()
}

func (c *Core) UnsubscribeStatus(ch chan RunInfo) {
	c.statusListener.Unsubscribe(ch)
}

func (c *Core) GetLogsFromRepository(ctx context.Context, runID string) ([]LogEntry, error) {
	repoLogs, err := c.repo.GetLogs(ctx, runID)
	if err != nil {
		return nil, err
	}

	logs := make([]LogEntry, len(repoLogs))
	for i, repoLog := range repoLogs {
		logs[i] = LogEntry{
			Timestamp: repoLog.Timestamp,
			Stream:    repoLog.Stream,
			Data:      repoLog.Data,
		}
	}

	return logs, nil
}
