package nomad

import (
	"context"
	"fmt"

	"github.com/hashicorp/nomad/api"
	"mise-ci/internal/runner"
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

	return &NomadRunner{
		client:  client,
		jobName: jobName,
	}, nil
}

func (n *NomadRunner) Dispatch(ctx context.Context, params runner.RunParams) (string, error) {
	meta := map[string]string{
		"callback_url": params.CallbackURL,
		"token":        params.Token,
	}
	if params.Image != "" {
		meta["image"] = params.Image
	}

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
