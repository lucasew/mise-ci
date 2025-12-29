package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

			// Generate fingerprint for the issue (Rule + Message + Tool)
			// Ideally we would use more stable properties, but this matches the request.
			fingerprintInput := fmt.Sprintf("%s|%s|%s", result.RuleID, result.Message.Text, toolName)
			hash := sha256.Sum256([]byte(fingerprintInput))
			issueID := hex.EncodeToString(hash[:])

			// 1. Upsert Issue
			if err := s.Core.repo.UpsertIssue(ctx, issueID, result.RuleID, result.Message.Text, severity, toolName); err != nil {
				return fmt.Errorf("failed to upsert issue: %w", err)
			}

			// 2. Create Occurrence
			if err := s.Core.repo.CreateOccurrence(ctx, issueID, runID, path, line); err != nil {
				return fmt.Errorf("failed to create occurrence: %w", err)
			}
		}
	}

	return nil
}
