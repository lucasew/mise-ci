package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/lucasew/mise-ci/internal/repository"
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

	var issues []repository.Issue
	var occurrences []repository.Occurrence

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

			issues = append(issues, repository.Issue{
				ID:       issueID,
				RuleID:   result.RuleID,
				Message:  result.Message.Text,
				Severity: severity,
				Tool:     toolName,
			})

			occurrences = append(occurrences, repository.Occurrence{
				IssueID: issueID,
				RunID:   runID,
				Path:    path,
				Line:    line,
			})
		}
	}

	if len(issues) > 0 {
		if err := s.Core.repo.BatchUpsertIssues(ctx, issues); err != nil {
			return fmt.Errorf("failed to batch upsert issues: %w", err)
		}
	}

	if len(occurrences) > 0 {
		if err := s.Core.repo.BatchCreateOccurrences(ctx, occurrences); err != nil {
			return fmt.Errorf("failed to batch create occurrences: %w", err)
		}
	}

	return nil
}
