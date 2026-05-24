package decisions

import (
	"testing"

	"github.com/ota-takeru/orchestrator/internal/verifier"
)

func TestEvaluateVerificationPassesWhenAllRequiredPass(t *testing.T) {
	results := EvaluateVerification(verifier.Report{RunID: "RUN-001", Results: []verifier.Result{
		{CommandID: "go-test", RequiredForMerge: true, Status: verifier.ResultPassed},
	}})
	if len(results) != 1 || results[0].Status != GatePass {
		t.Fatalf("unexpected gate results: %#v", results)
	}
}

func TestEvaluateVerificationOptionalFailureIsReportOnly(t *testing.T) {
	results := EvaluateVerification(verifier.Report{RunID: "RUN-001", Results: []verifier.Result{
		{CommandID: "smoke", RequiredForMerge: false, Status: verifier.ResultFailed},
	}})
	if len(results) != 1 || results[0].Status != GateReportOnly {
		t.Fatalf("unexpected gate results: %#v", results)
	}
}

func TestEvaluateVerificationRequiredCurrentDiffFailureAutoRepairs(t *testing.T) {
	failure := verifier.FailureCurrentDiff
	results := EvaluateVerification(verifier.Report{RunID: "RUN-001", Results: []verifier.Result{
		{CommandID: "go-test", RequiredForMerge: true, Status: verifier.ResultFailed, FailureClass: &failure},
	}})
	if len(results) != 1 || results[0].Status != GateAutoRepair {
		t.Fatalf("unexpected gate results: %#v", results)
	}
}

func TestEvaluateVerificationRequiredUnknownFailureNeedsDecision(t *testing.T) {
	failure := verifier.FailureUnknown
	results := EvaluateVerification(verifier.Report{RunID: "RUN-001", Results: []verifier.Result{
		{CommandID: "go-test", RequiredForMerge: true, Status: verifier.ResultFailed, FailureClass: &failure},
	}})
	if len(results) != 1 || results[0].Status != GateHumanDecision {
		t.Fatalf("unexpected gate results: %#v", results)
	}
}

func TestEvaluateVerificationRequiredBaselineFailureIsReportOnly(t *testing.T) {
	failure := verifier.FailureBaseline
	results := EvaluateVerification(verifier.Report{RunID: "RUN-001", Results: []verifier.Result{
		{CommandID: "go-test", RequiredForMerge: true, Status: verifier.ResultFailed, FailureClass: &failure},
	}})
	if len(results) != 1 || results[0].Status != GateReportOnly || results[0].Detector != "verification_failed_existing_baseline" {
		t.Fatalf("unexpected gate results: %#v", results)
	}
}
