package core

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	pb "github.com/lucasew/mise-ci/internal/proto"
)

type RunStatus string

const (
	StatusScheduled RunStatus = "scheduled"
	StatusRunning   RunStatus = "running"
	StatusSuccess   RunStatus = "success"
	StatusFailure   RunStatus = "failure"
	StatusError     RunStatus = "error"
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
}

type Core struct {
	runs           map[string]*Run
	runInfo        map[string]*RunInfo
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

func NewCore(logger *slog.Logger, secret string) *Core {
	return &Core{
		runs:           make(map[string]*Run),
		runInfo:        make(map[string]*RunInfo),
		logger:         logger,
		jwtSecret:      []byte(secret),
		logListener:    make(map[string]*ListenerManager[LogEntry]),
		statusListener: NewListenerManager[RunInfo](),
	}
}

func (c *Core) CreateRun(id string) *Run {
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

	info := &RunInfo{
		ID:        id,
		Status:    StatusScheduled,
		StartedAt: time.Now(),
		Logs:      make([]LogEntry, 0),
		UIToken:   uiToken,
	}
	c.runInfo[id] = info

	// Broadcast new run to status listeners
	infoCopy := *info
	infoCopy.Logs = nil
	c.statusListener.Broadcast(infoCopy)

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
	c.mu.RLock()
	info, ok := c.runInfo[runID]
	c.mu.RUnlock()

	if !ok || info.UIToken == "" {
		return ""
	}

	// Remove trailing slash if present
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	return fmt.Sprintf("%s/ui/run/%s?token=%s", baseURL, runID, info.UIToken)
}

func (c *Core) AddLog(runID string, stream string, data string) {
	c.mu.Lock()
	info, ok := c.runInfo[runID]
	if !ok {
		c.mu.Unlock()
		return
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Stream:    stream,
		Data:      data,
	}

	info.Logs = append(info.Logs, entry)

	lm, exists := c.logListener[runID]
	c.mu.Unlock()

	// Broadcast to listeners
	if exists {
		lm.Broadcast(entry)
	}
}

func (c *Core) UpdateStatus(runID string, status RunStatus, exitCode *int32) {
	c.mu.Lock()
	info, ok := c.runInfo[runID]
	if !ok {
		c.mu.Unlock()
		return
	}

	info.Status = status
	if exitCode != nil {
		info.ExitCode = exitCode
	}

	if status == StatusSuccess || status == StatusFailure || status == StatusError {
		now := time.Now()
		info.FinishedAt = &now
	}

	// Broadcast status change to all listeners
	infoCopy := *info
	infoCopy.Logs = nil // Don't send logs in status updates
	c.mu.Unlock()

	c.statusListener.Broadcast(infoCopy)
}

func (c *Core) GetRunInfo(runID string) (*RunInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info, ok := c.runInfo[runID]
	if !ok {
		return nil, false
	}

	// Return a copy to avoid race conditions
	infoCopy := *info
	infoCopy.Logs = make([]LogEntry, len(info.Logs))
	copy(infoCopy.Logs, info.Logs)

	return &infoCopy, true
}

func (c *Core) GetAllRuns() []RunInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	runs := make([]RunInfo, 0, len(c.runInfo))
	for _, info := range c.runInfo {
		infoCopy := *info
		infoCopy.Logs = nil // Don't include logs in list view
		runs = append(runs, infoCopy)
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
