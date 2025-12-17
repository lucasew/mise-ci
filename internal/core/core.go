package core

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/golang-jwt/jwt/v5"
	pb "mise-ci/internal/proto"
)

type Core struct {
	runs      map[string]*Run
	mu        sync.RWMutex
	logger    *slog.Logger
	jwtSecret []byte
}

type Run struct {
	ID        string
	CommandCh chan *pb.ServerMessage // Commands to send to worker
	ResultCh  chan *pb.WorkerMessage // Results from worker (for logic to consume)
	DoneCh    chan struct{}
}

func NewCore(logger *slog.Logger, secret string) *Core {
	return &Core{
		runs:      make(map[string]*Run),
		logger:    logger,
		jwtSecret: []byte(secret),
	}
}

func (c *Core) CreateRun(id string) *Run {
	c.mu.Lock()
	defer c.mu.Unlock()

	run := &Run{
		ID:        id,
		CommandCh: make(chan *pb.ServerMessage, 10),
		ResultCh:  make(chan *pb.WorkerMessage, 10),
		DoneCh:    make(chan struct{}),
	}
	c.runs[id] = run
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
