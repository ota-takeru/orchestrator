package decisions

import (
	"github.com/ota-takeru/orchestrator/internal/verifier"
)

type GateStatus string

const (
	GatePass          GateStatus = "PASS"
	GateAutoRepair    GateStatus = "AUTO_REPAIR"
	GateAutoReplan    GateStatus = "AUTO_REPLAN"
	GateReportOnly    GateStatus = "REPORT_ONLY"
	GateHumanInput    GateStatus = "HUMAN_INPUT"
	GateHumanDecision GateStatus = "HUMAN_DECISION"
	GateHardBlock     GateStatus = "HARD_BLOCK"
)

type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

type GateResult struct {
	Status          GateStatus `json:"status"`
	Severity        Severity   `json:"severity"`
	Detector        string     `json:"detector"`
	HumanActionType *string    `json:"human_action_type,omitempty"`
	Evidence        any        `json:"evidence"`
}

func EvaluateVerification(report verifier.Report) []GateResult {
	if len(report.Results) == 0 {
		action := "decision"
		return []GateResult{{
			Status:          GateHumanDecision,
			Severity:        SeverityHigh,
			Detector:        "verification_missing",
			HumanActionType: &action,
			Evidence:        map[string]any{"run_id": report.RunID},
		}}
	}

	var results []GateResult
	requiredFailure := false
	for _, result := range report.Results {
		if result.Status == verifier.ResultPassed || result.Status == verifier.ResultSkipped {
			continue
		}
		if !result.RequiredForMerge {
			results = append(results, GateResult{
				Status:   GateReportOnly,
				Severity: SeverityLow,
				Detector: "optional_verification_failed",
				Evidence: result,
			})
			continue
		}
		if result.FailureClass != nil && *result.FailureClass == verifier.FailureBaseline {
			results = append(results, GateResult{
				Status:   GateReportOnly,
				Severity: SeverityMedium,
				Detector: "verification_failed_existing_baseline",
				Evidence: result,
			})
			continue
		}
		requiredFailure = true
		if result.FailureClass != nil && *result.FailureClass == verifier.FailureCurrentDiff {
			results = append(results, GateResult{
				Status:   GateAutoRepair,
				Severity: SeverityHigh,
				Detector: "verification_failed_current_diff",
				Evidence: result,
			})
			continue
		}
		action := "decision"
		results = append(results, GateResult{
			Status:          GateHumanDecision,
			Severity:        SeverityHigh,
			Detector:        "required_verification_unclassified",
			HumanActionType: &action,
			Evidence:        result,
		})
	}
	if !requiredFailure && len(results) == 0 {
		results = append(results, GateResult{
			Status:   GatePass,
			Severity: SeverityLow,
			Detector: "verification_passed",
			Evidence: map[string]any{"run_id": report.RunID},
		})
	}
	return results
}
