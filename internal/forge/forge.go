package forge

import (
	"context"
	"io"
	"net/http"
)

type Forge interface {
	// ParseWebhook interprets webhook payload
	ParseWebhook(r *http.Request) (*WebhookEvent, error)

	// CloneCredentials returns temporary credentials for clone
	CloneCredentials(ctx context.Context, repo string) (*Credentials, error)

	// UpdateStatus updates commit status
	UpdateStatus(ctx context.Context, repo, sha string, status Status) error

	// UploadArtifact uploads run artifact
	UploadArtifact(ctx context.Context, repo string, runID string, name string, data io.Reader) error

	// UploadReleaseAsset uploads asset to release
	UploadReleaseAsset(ctx context.Context, repo, tag, name string, data io.Reader) error

	// GetCIEnv returns forge-specific environment variables for the event
	GetCIEnv(event *WebhookEvent) map[string]string
}

type WebhookEvent struct {
	Type  EventType // Push, PullRequest
	Repo  string    // owner/repo
	Ref   string    // refs/heads/main, refs/pull/123/merge
	SHA   string    // commit sha
	Clone string    // clone URL
}

type EventType int

const (
	EventTypePush EventType = iota
	EventTypePullRequest
)

type Status struct {
	State       State  // Pending, Success, Failure, Error
	Context     string // "mise-ci"
	Description string
	TargetURL   string // link to logs
}

type State int

const (
	StatePending State = iota
	StateSuccess
	StateFailure
	StateError
)

type Credentials struct {
	Token string
}
