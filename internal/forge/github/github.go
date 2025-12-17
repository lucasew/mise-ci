package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v66/github"
	"github.com/lucasew/mise-ci/internal/forge"
)

type GitHubForge struct {
	appID         int64
	privateKey    []byte
	webhookSecret []byte
}

func NewGitHubForge(appID int64, privateKey []byte, webhookSecret string) *GitHubForge {
	return &GitHubForge{
		appID:         appID,
		privateKey:    privateKey,
		webhookSecret: []byte(webhookSecret),
	}
}

func (g *GitHubForge) ParseWebhook(r *http.Request) (*forge.WebhookEvent, error) {
	payload, err := github.ValidatePayload(r, g.webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("validate payload: %w", err)
	}

	event, err := github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		return nil, fmt.Errorf("parse webhook: %w", err)
	}

	switch e := event.(type) {
	case *github.PushEvent:
		if e.Repo == nil || e.Repo.FullName == nil || e.Ref == nil || e.After == nil {
			return nil, nil // Invalid or missing data
		}
		return &forge.WebhookEvent{
			Type:  forge.EventTypePush,
			Repo:  *e.Repo.FullName,
			Ref:   *e.Ref,
			SHA:   *e.After,
			Clone: e.Repo.GetCloneURL(),
		}, nil
	case *github.PullRequestEvent:
		if e.Action == nil || (*e.Action != "opened" && *e.Action != "synchronize") {
			return nil, nil
		}
		if e.Repo == nil || e.Repo.FullName == nil || e.PullRequest == nil || e.PullRequest.Head == nil {
			return nil, nil
		}
		return &forge.WebhookEvent{
			Type:  forge.EventTypePullRequest,
			Repo:  *e.Repo.FullName,
			Ref:   fmt.Sprintf("refs/pull/%d/head", e.GetNumber()),
			SHA:   *e.PullRequest.Head.SHA,
			Clone: e.Repo.GetCloneURL(),
		}, nil
	}

	return nil, nil
}

func (g *GitHubForge) CloneCredentials(ctx context.Context, repo string) (*forge.Credentials, error) {
	client, err := g.getAppClient()
	if err != nil {
		return nil, err
	}

	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s", repo)
	}
	owner, name := parts[0], parts[1]

	inst, _, err := client.Apps.FindRepositoryInstallation(ctx, owner, name)
	if err != nil {
		return nil, fmt.Errorf("find installation: %w", err)
	}

	token, _, err := client.Apps.CreateInstallationToken(ctx, inst.GetID(), nil)
	if err != nil {
		return nil, fmt.Errorf("create installation token: %w", err)
	}

	return &forge.Credentials{
		Token: token.GetToken(),
	}, nil
}

func (g *GitHubForge) UpdateStatus(ctx context.Context, repo, sha string, status forge.Status) error {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo format: %s", repo)
	}
	owner, name := parts[0], parts[1]

	// Need installation client
	// Optimization: could cache installation tokens
	// For now, create a new one every time
	creds, err := g.CloneCredentials(ctx, repo)
	if err != nil {
		return err
	}

	client := github.NewClient(nil).WithAuthToken(creds.Token)

	ghState := ""
	switch status.State {
	case forge.StatePending:
		ghState = "pending"
	case forge.StateSuccess:
		ghState = "success"
	case forge.StateFailure:
		ghState = "failure"
	case forge.StateError:
		ghState = "error"
	}

	_, _, err = client.Repositories.CreateStatus(ctx, owner, name, sha, &github.RepoStatus{
		State:       &ghState,
		Context:     &status.Context,
		Description: &status.Description,
		TargetURL:   &status.TargetURL,
	})
	return err
}

func (g *GitHubForge) UploadArtifact(ctx context.Context, repo string, runID string, name string, data io.Reader) error {
	return fmt.Errorf("not implemented")
}

func (g *GitHubForge) UploadReleaseAsset(ctx context.Context, repo, tag, name string, data io.Reader) error {
	return fmt.Errorf("not implemented")
}

func (g *GitHubForge) getAppClient() (*github.Client, error) {
	// Create JWT
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": g.appID,
		"iat": time.Now().Add(-1 * time.Minute).Unix(),
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"alg": "RS256",
	})

	key, err := jwt.ParseRSAPrivateKeyFromPEM(g.privateKey)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	signed, err := token.SignedString(key)
	if err != nil {
		return nil, fmt.Errorf("sign jwt: %w", err)
	}

	// Create client that sends this JWT
	// Using a custom RoundTripper is cleaner but for simplicity we can just set headers manually or use library features if available?
	// go-github doesn't support Bearer token auth easily without WithAuthToken which sets Authorization: Bearer <token>
	// which is what we need for JWT too (Authorization: Bearer <jwt>)

	client := github.NewClient(nil).WithAuthToken(signed)
	return client, nil
}
