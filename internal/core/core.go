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
}

type Core struct {
	runs           map[string]*Run
	runInfo        map[string]*RunInfo
	mu             sync.RWMutex
	logger         *slog.Logger
	jwtSecret      []byte
	logListener    map[string][]chan LogEntry // run_id -> list of log listeners
	statusListener []chan RunInfo             // global status change listeners
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
		logListener:    make(map[string][]chan LogEntry),
		statusListener: make([]chan RunInfo, 0),
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

	info := &RunInfo{
		ID:        id,
		Status:    StatusScheduled,
		StartedAt: time.Now(),
		Logs:      make([]LogEntry, 0),
	}
	c.runInfo[id] = info

	// Broadcast new run to status listeners
	infoCopy := *info
	infoCopy.Logs = nil
	for _, ch := range c.statusListener {
		select {
		case ch <- infoCopy:
		default:
			// Skip if channel is full
		}
	}

	return run
}

func (c *Core) GetRun(id string) (*Run, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	run, ok := c.runs[id]
	return run, ok
}

func (c *Core) ValidateToken(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return c.jwtSecret, nil
	})

	if err != nil {
		return "", err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if runID, ok := claims["run_id"].(string); ok {
			return runID, nil
		}
		return "", fmt.Errorf("run_id claim missing or invalid")
	}

	return "", fmt.Errorf("invalid token")
}

func (c *Core) GenerateToken(runID string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"run_id": runID,
	})
	return token.SignedString(c.jwtSecret)
}

func (c *Core) AddLog(runID string, stream string, data string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	info, ok := c.runInfo[runID]
	if !ok {
		return
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Stream:    stream,
		Data:      data,
	}

	info.Logs = append(info.Logs, entry)

	// Broadcast to listeners
	if listeners, ok := c.logListener[runID]; ok {
		for _, ch := range listeners {
			select {
			case ch <- entry:
			default:
				// Skip if channel is full
			}
		}
	}
}

func (c *Core) UpdateStatus(runID string, status RunStatus, exitCode *int32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	info, ok := c.runInfo[runID]
	if !ok {
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
	for _, ch := range c.statusListener {
		select {
		case ch <- infoCopy:
		default:
			// Skip if channel is full
		}
	}
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

	ch := make(chan LogEntry, 100)
	c.logListener[runID] = append(c.logListener[runID], ch)
	return ch
}

func (c *Core) UnsubscribeLogs(runID string, ch chan LogEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	listeners := c.logListener[runID]
	for i, listener := range listeners {
		if listener == ch {
			c.logListener[runID] = append(listeners[:i], listeners[i+1:]...)
			close(ch)
			break
		}
	}
}

func (c *Core) SubscribeStatus() chan RunInfo {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch := make(chan RunInfo, 100)
	c.statusListener = append(c.statusListener, ch)
	return ch
}

func (c *Core) UnsubscribeStatus(ch chan RunInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, listener := range c.statusListener {
		if listener == ch {
			c.statusListener = append(c.statusListener[:i], c.statusListener[i+1:]...)
			close(ch)
			break
		}
	}
}
