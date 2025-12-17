package orchestration

import (
	"fmt"
	"log/slog"
)

// LogProvider defines the interface needed for logging
type LogProvider interface {
	AddLog(runID string, stream string, data string)
}

// Step represents a single step in the orchestration pipeline
type Step struct {
	Name        string
	Description string
	Execute     func() error
}

// Pipeline executes a series of steps with automatic logging
type Pipeline struct {
	runID       string
	logProvider LogProvider
	logger      *slog.Logger
	steps       []Step
}

// NewPipeline creates a new orchestration pipeline
func NewPipeline(runID string, logProvider LogProvider, logger *slog.Logger) *Pipeline {
	return &Pipeline{
		runID:       runID,
		logProvider: logProvider,
		logger:      logger,
		steps:       make([]Step, 0),
	}
}

// AddStep adds a step to the pipeline
func (p *Pipeline) AddStep(name, description string, fn func() error) *Pipeline {
	p.steps = append(p.steps, Step{
		Name:        name,
		Description: description,
		Execute:     fn,
	})
	return p
}

// Run executes all steps in order
func (p *Pipeline) Run() error {
	p.logger.Info("starting pipeline", "run_id", p.runID, "steps", len(p.steps))

	for i, step := range p.steps {
		p.logger.Info("executing step", "run_id", p.runID, "step", step.Name, "description", step.Description)
		p.logProvider.AddLog(p.runID, "system", fmt.Sprintf("[%d/%d] %s...", i+1, len(p.steps), step.Description))

		if err := step.Execute(); err != nil {
			p.logger.Error("step failed", "run_id", p.runID, "step", step.Name, "error", err)
			p.logProvider.AddLog(p.runID, "system", fmt.Sprintf("Step '%s' failed: %v", step.Name, err))
			return fmt.Errorf("step %s failed: %w", step.Name, err)
		}

		p.logProvider.AddLog(p.runID, "system", fmt.Sprintf("Step '%s' completed successfully", step.Name))
	}

	p.logger.Info("pipeline completed successfully", "run_id", p.runID)
	p.logProvider.AddLog(p.runID, "system", "Pipeline completed successfully")
	return nil
}
