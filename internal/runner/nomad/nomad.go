package nomad

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/nomad/api"
	"mise-ci/internal/runner"
)

type NomadRunner struct {
	client *api.Client
	jobID  string
}

func NewNomadRunner(addr string, templatePath string) (*NomadRunner, error) {
	config := api.DefaultConfig()
	if addr != "" {
		config.Address = addr
	}
	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("create nomad client: %w", err)
	}

	// Register job
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("read job template: %w", err)
	}

	job, err := client.Jobs().ParseHCL(string(data), true)
	if err != nil {
		return nil, fmt.Errorf("parse job template: %w", err)
	}

	if _, _, err := client.Jobs().Register(job, nil); err != nil {
		return nil, fmt.Errorf("register job: %w", err)
	}

	return &NomadRunner{
		client: client,
		jobID:  *job.ID,
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

	resp, _, err := n.client.Jobs().Dispatch(n.jobID, meta, nil, "", nil)
	if err != nil {
		return "", fmt.Errorf("dispatch job: %w", err)
	}

	return resp.DispatchedJobID, nil
}

func (n *NomadRunner) Cancel(ctx context.Context, jobID string) error {
	_, _, err := n.client.Jobs().Deregister(jobID, true, nil)
	return err
}
