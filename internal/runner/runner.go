package runner

import "context"

type Runner interface {
	// Dispatch spawns a job for a run and returns the job ID (or dispatch ID)
	Dispatch(ctx context.Context, params RunParams) (string, error)

	// Cancel cancels a running job
	Cancel(ctx context.Context, jobID string) error
}

type RunParams struct {
	CallbackURL string // URL of the matrix
	Token       string // JWT for auth
	Image       string // Container image (optional)
}
