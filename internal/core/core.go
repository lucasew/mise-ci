package core

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lucasew/mise-ci/internal/forge"
	pb "github.com/lucasew/mise-ci/internal/proto"
	"github.com/lucasew/mise-ci/internal/repository"
)

type RunStatus string

const (
	StatusScheduled  RunStatus = "scheduled"
	StatusDispatched RunStatus = "dispatched"
	StatusRunning    RunStatus = "running"
	StatusSuccess    RunStatus = "success"
	StatusFailure    RunStatus = "failure"
	StatusError      RunStatus = "error"
	StatusSkipped    RunStatus = "skipped"
)

type TokenType string

const (
	TokenTypeWorker     TokenType = "worker"      // Legacy: worker para run específica
	TokenTypePoolWorker TokenType = "pool_worker" // Novo: worker do pool sem run específica
	TokenTypeUI         TokenType = "ui"
)

type LogEntry struct {
	Timestamp time.Time
	Stream    string // "stdout", "stderr", "system"
	Data      string
}

type RunInfo struct {
	ID            string
	Status        RunStatus
	StartedAt     time.Time
	FinishedAt    *time.Time
	Logs          []LogEntry
	ExitCode      *int32
	UIToken       string
	GitLink       string
	RepoURL       string
	CommitMessage string
	Author        string
	Branch        string
}

type LogBuffer struct {
	mu      sync.Mutex
	entries []LogEntry
	core    *Core
	runID   string
	flushCh chan struct{}
}

func (lb *LogBuffer) start() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lb.flush()
		case <-lb.flushCh:
			lb.flush()
			return
		}
	}
}

func (lb *LogBuffer) flush() {
	lb.mu.Lock()
	if len(lb.entries) == 0 {
		lb.mu.Unlock()
		return
	}
	entries := lb.entries
	lb.entries = nil
	lb.mu.Unlock()

	lb.core.flushLogs(lb.runID, entries)
}

func (lb *LogBuffer) append(entry LogEntry) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.entries = append(lb.entries, entry)
}

type Core struct {
	runs              map[string]*Run
	repo              repository.Repository
	forge             forge.Forge
	mu                sync.RWMutex
	logger            *slog.Logger
	jwtSecret         []byte
	logListener       map[string]*ListenerManager[[]LogEntry] // run_id -> log listeners manager
	statusListener    *ListenerManager[RunInfo]               // global status change listeners manager
	logBuffers        map[string]*LogBuffer
	StartTime         time.Time
	dispatchedQueue   []string      // Fila de runs dispatched aguardando worker
	scheduledQueue    []string      // Fila de runs scheduled aguardando worker
	queueMu           sync.Mutex    // Mutex para acesso à fila
	waitingWorkers    chan struct{} // Canal para sinalizar workers aguardando
	waitingWorkersMu  sync.Mutex
	waitingWorkersMap map[chan string]struct{} // Conjunto de workers aguardando runs
}

type Run struct {
	ID          string
	CommandCh   chan *pb.ServerMessage // Commands to send to worker
	ResultCh    chan *pb.WorkerMessage // Results from worker (for logic to consume)
	DoneCh      chan struct{}
	ConnectedCh chan struct{} // Signals when worker connects
	RetryCh     chan struct{} // Signals when a retry is needed (e.g. handshake failure)
	Env         map[string]string
	NextOpID    atomic.Uint64
}

func NewCore(logger *slog.Logger, secret string, repo repository.Repository) *Core {
	return &Core{
		runs:              make(map[string]*Run),
		repo:              repo,
		logger:            logger,
		jwtSecret:         []byte(secret),
		logListener:       make(map[string]*ListenerManager[[]LogEntry]),
		statusListener:    NewListenerManager[RunInfo](),
		logBuffers:        make(map[string]*LogBuffer),
		StartTime:         time.Now(),
		dispatchedQueue:   make([]string, 0),
		scheduledQueue:    make([]string, 0),
		waitingWorkers:    make(chan struct{}, 100),
		waitingWorkersMap: make(map[chan string]struct{}),
	}
}

func (c *Core) SetForge(f forge.Forge) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forge = f
}

func (c *Core) CreateRun(id string, gitLink, repoURL, commitMessage, author, branch string) *Run {
	c.mu.Lock()
	defer c.mu.Unlock()

	run := &Run{
		ID:          id,
		CommandCh:   make(chan *pb.ServerMessage, 10),
		ResultCh:    make(chan *pb.WorkerMessage, 10),
		DoneCh:      make(chan struct{}),
		ConnectedCh: make(chan struct{}),
		RetryCh:     make(chan struct{}, 1),
		Env:         make(map[string]string),
	}
	c.runs[id] = run

	uiToken, err := c.generateToken(id, TokenTypeUI)
	if err != nil {
		c.logger.Error("failed to generate UI token", "error", err, "run_id", id)
		// We continue without a token, but this shouldn't happen
	}

	// Save to database
	metadata := &repository.RunMetadata{
		ID:            id,
		Status:        string(StatusScheduled),
		StartedAt:     time.Now(),
		UIToken:       uiToken,
		GitLink:       gitLink,
		RepoURL:       repoURL,
		CommitMessage: commitMessage,
		Author:        author,
		Branch:        branch,
	}

	ctx := context.Background()
	if err := c.repo.CreateRun(ctx, metadata); err != nil {
		c.logger.Error("failed to create run in database", "error", err, "run_id", id)
	}

	// Broadcast to status listeners
	c.statusListener.Broadcast(RunInfo{
		ID:            id,
		Status:        StatusScheduled,
		StartedAt:     metadata.StartedAt,
		UIToken:       uiToken,
		GitLink:       gitLink,
		RepoURL:       repoURL,
		CommitMessage: commitMessage,
		Author:        author,
		Branch:        branch,
	})

	// Adiciona à fila de runs aguardando worker
	c.EnqueueRun(id, StatusScheduled)

	return run
}

func (c *Core) GetRun(id string) (*Run, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	run, ok := c.runs[id]
	return run, ok
}

func (c *Core) SetRunEnv(id string, env map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if run, ok := c.runs[id]; ok {
		run.Env = env
	}
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
		tokenTypeStr, ok := claims["type"].(string)
		var tokenType TokenType
		if !ok {
			// Backwards compatibility: assume worker token if type is missing
			tokenType = TokenTypeWorker
		} else {
			tokenType = TokenType(tokenTypeStr)
		}

		// Pool workers não têm run_id no token
		if tokenType == TokenTypePoolWorker {
			return "", tokenType, nil
		}

		// Outros tipos de token exigem run_id
		runID, ok := claims["run_id"].(string)
		if !ok {
			return "", "", fmt.Errorf("run_id claim missing or invalid")
		}

		return runID, tokenType, nil
	}

	return "", "", fmt.Errorf("invalid token")
}

func (c *Core) GenerateWorkerToken(runID string) (string, error) {
	return c.generateToken(runID, TokenTypeWorker)
}

func (c *Core) GeneratePoolWorkerToken() (string, error) {
	return c.generatePoolToken(TokenTypePoolWorker)
}

func (c *Core) GenerateUIToken(runID string) (string, error) {
	return c.generateToken(runID, TokenTypeUI)
}

func (c *Core) generateToken(runID string, tokenType TokenType) (string, error) {
	now := time.Now()
	var expiresAt *jwt.NumericDate
	if tokenType == TokenTypeUI {
		expiresAt = jwt.NewNumericDate(now.Add(24 * time.Hour))
	} else {
		expiresAt = jwt.NewNumericDate(now.Add(1 * time.Hour))
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"run_id": runID,
		"type":   tokenType,
		"exp":    expiresAt,
		"nbf":    jwt.NewNumericDate(now),
		"iat":    jwt.NewNumericDate(now),
	})
	return token.SignedString(c.jwtSecret)
}

func (c *Core) generatePoolToken(tokenType TokenType) (string, error) {
	now := time.Now()
	expiresAt := jwt.NewNumericDate(now.Add(1 * time.Hour))

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"type": tokenType,
		"exp":  expiresAt,
		"nbf":  jwt.NewNumericDate(now),
		"iat":  jwt.NewNumericDate(now),
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

func (c *Core) getOrCreateLogBuffer(runID string) *LogBuffer {
	c.mu.Lock()
	defer c.mu.Unlock()

	if lb, ok := c.logBuffers[runID]; ok {
		return lb
	}

	lb := &LogBuffer{
		core:    c,
		runID:   runID,
		flushCh: make(chan struct{}),
	}
	c.logBuffers[runID] = lb
	go lb.start()
	return lb
}

func (c *Core) AddLog(runID string, stream string, data string) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Stream:    stream,
		Data:      data,
	}

	lb := c.getOrCreateLogBuffer(runID)
	lb.append(entry)
}

func (c *Core) flushLogs(runID string, entries []LogEntry) {
	if len(entries) == 0 {
		return
	}

	// Save to database (batch)
	ctx := context.Background()
	repoEntries := make([]repository.LogEntry, len(entries))
	for i, entry := range entries {
		repoEntries[i] = repository.LogEntry{
			Timestamp: entry.Timestamp,
			Stream:    entry.Stream,
			Data:      entry.Data,
		}
	}

	if err := c.repo.AppendLogs(ctx, runID, repoEntries); err != nil {
		c.logger.Error("failed to persist logs", "error", err, "run_id", runID)
	}

	// Broadcast to listeners
	c.mu.RLock()
	lm, exists := c.logListener[runID]
	c.mu.RUnlock()

	if exists {
		lm.Broadcast(entries)
	}
}

func (c *Core) UpdateStatus(runID string, status RunStatus, exitCode *int32) {
	var lb *LogBuffer
	// Check if finished and flush logs
	if status == StatusSuccess || status == StatusFailure || status == StatusError || status == StatusSkipped {
		c.mu.Lock()
		if l, ok := c.logBuffers[runID]; ok {
			lb = l
			delete(c.logBuffers, runID)
		}
		c.mu.Unlock()
		if lb != nil {
			lb.flush()
			close(lb.flushCh)
		}
	}

	// Update in database
	ctx := context.Background()
	if err := c.repo.UpdateRunStatus(ctx, runID, string(status), exitCode); err != nil {
		c.logger.Error("failed to update status in database", "error", err, "run_id", runID)
		return
	}

	// Gerenciar filas baseado na mudança de status
	c.manageQueueOnStatusChange(runID, status)

	// Get updated run info for broadcast
	meta, err := c.repo.GetRun(ctx, runID)
	if err != nil {
		c.logger.Error("failed to get run after status update", "error", err, "run_id", runID)
		return
	}

	// Broadcast status change to listeners
	c.statusListener.Broadcast(RunInfo{
		ID:            meta.ID,
		Status:        RunStatus(meta.Status),
		StartedAt:     meta.StartedAt,
		FinishedAt:    meta.FinishedAt,
		ExitCode:      meta.ExitCode,
		UIToken:       meta.UIToken,
		GitLink:       meta.GitLink,
		RepoURL:       meta.RepoURL,
		CommitMessage: meta.CommitMessage,
		Author:        meta.Author,
		Branch:        meta.Branch,
	})
}

func (c *Core) GetRunInfo(runID string) (*RunInfo, bool) {
	ctx := context.Background()

	meta, err := c.repo.GetRun(ctx, runID)
	if err != nil {
		return nil, false
	}

	return &RunInfo{
		ID:            meta.ID,
		Status:        RunStatus(meta.Status),
		StartedAt:     meta.StartedAt,
		FinishedAt:    meta.FinishedAt,
		ExitCode:      meta.ExitCode,
		UIToken:       meta.UIToken,
		GitLink:       meta.GitLink,
		RepoURL:       meta.RepoURL,
		CommitMessage: meta.CommitMessage,
		Author:        meta.Author,
		Branch:        meta.Branch,
		Logs:          nil, // Logs fetched separately via GetLogsFromRepository
	}, true
}

func (c *Core) GetAllRuns() []RunInfo {
	return c.ListRuns(repository.RunFilter{Limit: 100})
}

func (c *Core) GetRepos() []string {
	ctx := context.Background()
	repos, err := c.repo.ListRepos(ctx)
	if err != nil {
		c.logger.Error("failed to list repos", "error", err)
		return []string{}
	}
	return repos
}

func (c *Core) ListRuns(filter repository.RunFilter) []RunInfo {
	ctx := context.Background()

	repoRuns, err := c.repo.ListRuns(ctx, filter)
	if err != nil {
		c.logger.Error("failed to list runs from repository", "error", err)
		return []RunInfo{}
	}

	runs := make([]RunInfo, len(repoRuns))
	for i, repoRun := range repoRuns {
		runs[i] = RunInfo{
			ID:            repoRun.ID,
			Status:        RunStatus(repoRun.Status),
			StartedAt:     repoRun.StartedAt,
			FinishedAt:    repoRun.FinishedAt,
			Logs:          nil,
			ExitCode:      repoRun.ExitCode,
			UIToken:       repoRun.UIToken,
			GitLink:       repoRun.GitLink,
			RepoURL:       repoRun.RepoURL,
			CommitMessage: repoRun.CommitMessage,
			Author:        repoRun.Author,
			Branch:        repoRun.Branch,
		}
	}

	return runs
}

func (c *Core) SubscribeLogs(runID string) chan []LogEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.logListener[runID]; !ok {
		c.logListener[runID] = NewListenerManager[[]LogEntry]()
	}
	return c.logListener[runID].Subscribe()
}

func (c *Core) UnsubscribeLogs(runID string, ch chan []LogEntry) {
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

// EnqueueRun adiciona uma run à fila apropriada baseada no status
func (c *Core) EnqueueRun(runID string, status RunStatus) {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	if status == StatusDispatched {
		c.dispatchedQueue = append(c.dispatchedQueue, runID)
		c.logger.Info("run enqueued", "run_id", runID, "queue", "dispatched", "queue_size", len(c.dispatchedQueue))
	} else if status == StatusScheduled {
		c.scheduledQueue = append(c.scheduledQueue, runID)
		c.logger.Info("run enqueued", "run_id", runID, "queue", "scheduled", "queue_size", len(c.scheduledQueue))
	}

	// Notifica workers aguardando
	select {
	case c.waitingWorkers <- struct{}{}:
	default:
	}
}

// DequeueRun pega a próxima run da fila (prioriza dispatched sobre scheduled)
func (c *Core) DequeueRun() (string, RunStatus, bool) {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	// Prioriza runs dispatched
	if len(c.dispatchedQueue) > 0 {
		runID := c.dispatchedQueue[0]
		c.dispatchedQueue = c.dispatchedQueue[1:]
		c.logger.Info("run dequeued", "run_id", runID, "queue", "dispatched", "remaining", len(c.dispatchedQueue))
		return runID, StatusDispatched, true
	}

	// Se não há dispatched, pega da fila scheduled
	if len(c.scheduledQueue) > 0 {
		runID := c.scheduledQueue[0]
		c.scheduledQueue = c.scheduledQueue[1:]
		c.logger.Info("run dequeued", "run_id", runID, "queue", "scheduled", "remaining", len(c.scheduledQueue))
		return runID, StatusScheduled, true
	}

	return "", "", false
}

// WaitForRun aguarda uma run ficar disponível na fila
func (c *Core) WaitForRun(ctx context.Context) (string, RunStatus, error) {
	// Primeiro tenta pegar uma run da fila
	if runID, status, ok := c.DequeueRun(); ok {
		return runID, status, nil
	}

	// Se não há runs, aguarda notificação
	select {
	case <-c.waitingWorkers:
		// Tenta novamente após notificação
		if runID, status, ok := c.DequeueRun(); ok {
			return runID, status, nil
		}
		return "", "", fmt.Errorf("no runs available after notification")
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

// manageQueueOnStatusChange gerencia as filas quando o status muda
func (c *Core) manageQueueOnStatusChange(runID string, newStatus RunStatus) {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	switch newStatus {
	case StatusDispatched:
		// Remove da fila scheduled e adiciona à dispatched
		c.removeFromQueue(&c.scheduledQueue, runID)
		// Só adiciona se não estiver já na fila dispatched
		if !c.isInQueue(c.dispatchedQueue, runID) {
			c.dispatchedQueue = append(c.dispatchedQueue, runID)
			c.logger.Info("run moved to dispatched queue", "run_id", runID)
		}
	case StatusRunning, StatusSuccess, StatusFailure, StatusError, StatusSkipped:
		// Remove de ambas as filas quando começa a executar ou termina
		c.removeFromQueue(&c.scheduledQueue, runID)
		c.removeFromQueue(&c.dispatchedQueue, runID)
		c.logger.Info("run removed from queues", "run_id", runID, "status", newStatus)
	}
}

// removeFromQueue remove um runID de uma fila
func (c *Core) removeFromQueue(queue *[]string, runID string) {
	for i, id := range *queue {
		if id == runID {
			*queue = append((*queue)[:i], (*queue)[i+1:]...)
			return
		}
	}
}

// isInQueue verifica se um runID está na fila
func (c *Core) isInQueue(queue []string, runID string) bool {
	for _, id := range queue {
		if id == runID {
			return true
		}
	}
	return false
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

func (c *Core) ListFindingsForRepo(ctx context.Context, repoURL string, limit int) ([]repository.SarifFinding, error) {
	return c.repo.ListFindingsForRepo(ctx, repoURL, limit)
}
