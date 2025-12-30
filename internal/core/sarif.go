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

	var rules []repository.Rule
	var findings []repository.Finding

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

			// Generate rule ID (hash of rule_id + tool)
			ruleFingerprintInput := fmt.Sprintf("%s|%s", result.RuleID, toolName)
			hash := sha256.Sum256([]byte(ruleFingerprintInput))
			ruleID := hex.EncodeToString(hash[:])

			rules = append(rules, repository.Rule{
				ID:       ruleID,
				RuleID:   result.RuleID,
				Severity: severity,
				Tool:     toolName,
			})

			findings = append(findings, repository.Finding{
				RunID:   runID,
				RuleRef: ruleID,
				Message: result.Message.Text,
				Path:    path,
				Line:    line,
			})
		}
	}

	if len(rules) > 0 {
		if err := s.Core.repo.BatchUpsertRules(ctx, rules); err != nil {
			return fmt.Errorf("failed to batch upsert rules: %w", err)
		}
	}

	if len(findings) > 0 {
		if err := s.Core.repo.BatchCreateFindings(ctx, findings); err != nil {
			return fmt.Errorf("failed to batch create findings: %w", err)
		}
	}

	return nil
}
