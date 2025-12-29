package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Minimal SARIF structure to extract what we need
type SarifReport struct {
	Runs []SarifRun `json:"runs"`
}

type SarifRun struct {
	Tool struct {
		Driver struct {
			Name string `json:"name"`
		} `json:"driver"`
	} `json:"tool"`
	Results []SarifResult `json:"results"`
}

type SarifResult struct {
	RuleID  string `json:"ruleId"`
	Message struct {
		Text string `json:"text"`
	} `json:"message"`
	Level     string `json:"level"` // "warning", "error", "note", "none"
	Locations []struct {
		PhysicalLocation struct {
			ArtifactLocation struct {
				URI string `json:"uri"`
			} `json:"artifactLocation"`
			Region struct {
				StartLine int `json:"startLine"`
			} `json:"region"`
		} `json:"physicalLocation"`
	} `json:"locations"`
}

func (s *Service) IngestSARIF(ctx context.Context, runID string, data []byte) error {
	var report SarifReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("failed to parse sarif: %w", err)
	}

	for _, run := range report.Runs {
		toolName := run.Tool.Driver.Name
		if toolName == "" {
			toolName = "unknown"
		}

		// Create SARIF run record
		sarifRunID := fmt.Sprintf("%s-%s-%d", runID, toolName, time.Now().UnixNano())

		if err := s.Core.repo.CreateSarifRun(ctx, sarifRunID, runID, toolName); err != nil {
			return fmt.Errorf("failed to create sarif run: %w", err)
		}

		for _, result := range run.Results {
			path := ""
			line := 0
			if len(result.Locations) > 0 {
				path = result.Locations[0].PhysicalLocation.ArtifactLocation.URI
				line = result.Locations[0].PhysicalLocation.Region.StartLine
			}

			severity := result.Level
			if severity == "" {
				severity = "warning" // default
			}

			if err := s.Core.repo.CreateSarifIssue(ctx, sarifRunID, result.RuleID, result.Message.Text, path, line, severity); err != nil {
				return fmt.Errorf("failed to create sarif issue: %w", err)
			}
		}
	}

	return nil
}
