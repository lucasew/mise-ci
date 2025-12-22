package nomad

import (
	"context"
	"fmt"

	"github.com/hashicorp/nomad/api"
	"github.com/lucasew/mise-ci/internal/runner"
)

type NomadRunner struct {
	client  *api.Client
	jobName string
}

func NewNomadRunner(addr string, jobName string) (*NomadRunner, error) {
	config := api.DefaultConfig()
	if addr != "" {
		config.Address = addr
	}
	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("create nomad client: %w", err)
	}

	nr := &NomadRunner{
		client:  client,
		jobName: jobName,
	}

	// Verify the job exists and is parameterized
	if err := nr.VerifyJob(); err != nil {
		return nil, err
	}

	return nr, nil
}

func (n *NomadRunner) VerifyJob() error {
	job, _, err := n.client.Jobs().Info(n.jobName, nil)
	if err != nil {
		return fmt.Errorf("failed to get job info: %w (make sure the parameterized job '%s' exists in Nomad)", err, n.jobName)
	}

	if job.ParameterizedJob == nil {
		return fmt.Errorf("job '%s' exists but is not a parameterized job", n.jobName)
	}

	return nil
}

func (n *NomadRunner) Dispatch(ctx context.Context, params runner.RunParams) (string, error) {
	meta := map[string]string{
		"callback_url": params.CallbackURL,
		"token":        params.Token,
	}
	if params.Image != "" {
		meta["image"] = params.Image
	}
	// GitHubToken is no longer passed via Nomad dispatch
	// Worker will request it from the server after handshake

	resp, _, err := n.client.Jobs().Dispatch(n.jobName, meta, nil, "", nil)
	if err != nil {
		return "", fmt.Errorf("dispatch job: %w", err)
	}

	return resp.DispatchedJobID, nil
}

func (n *NomadRunner) Cancel(ctx context.Context, jobID string) error {
	_, _, err := n.client.Jobs().Deregister(jobID, true, nil)
	return err
}
