package verifier

import (
	"context"
	"fmt"
	"time"

	"github.com/ota-takeru/orchestrator/internal/runners"
)

type ResultStatus string

const (
	ResultPassed  ResultStatus = "passed"
	ResultFailed  ResultStatus = "failed"
	ResultSkipped ResultStatus = "skipped"
	ResultError   ResultStatus = "error"
)

type FailureClass string

const (
	FailureCurrentDiff FailureClass = "current_diff"
	FailureEnvironment FailureClass = "environment"
	FailureBaseline    FailureClass = "baseline"
	FailureSpecGap     FailureClass = "spec_gap"
	FailureUnknown     FailureClass = "unknown"
)

type Command struct {
	ID               string                `json:"id"`
	EnvironmentID    string                `json:"environment_id"`
	Runner           string                `json:"runner"`
	WorkingDir       string                `json:"working_dir"`
	Argv             []string              `json:"argv"`
	Timeout          time.Duration         `json:"timeout"`
	NetworkPolicy    runners.NetworkPolicy `json:"network_policy"`
	RequiredForMerge bool                  `json:"required_for_merge"`
}

type Result struct {
	CommandID        string                   `json:"command_id"`
	EnvironmentID    string                   `json:"environment_id"`
	RequiredForMerge bool                     `json:"required_for_merge"`
	Status           ResultStatus             `json:"status"`
	FailureClass     *FailureClass            `json:"failure_class,omitempty"`
	Message          string                   `json:"message,omitempty"`
	CommandResult    runners.RunCommandResult `json:"command_result"`
}

type Report struct {
	RunID   string   `json:"run_id"`
	Results []Result `json:"results"`
}

func (r Report) RequiredFailureCount() int {
	count := 0
	for _, result := range r.Results {
		if result.RequiredForMerge && (result.Status == ResultFailed || result.Status == ResultError) {
			count++
		}
	}
	return count
}

func (r Report) BlockingRequiredFailureCount() int {
	count := 0
	for _, result := range r.Results {
		if !result.RequiredForMerge || (result.Status != ResultFailed && result.Status != ResultError) {
			continue
		}
		if result.FailureClass != nil && *result.FailureClass == FailureBaseline {
			continue
		}
		count++
	}
	return count
}

func (r Report) OptionalFailureCount() int {
	count := 0
	for _, result := range r.Results {
		if !result.RequiredForMerge && (result.Status == ResultFailed || result.Status == ResultError) {
			count++
		}
	}
	return count
}

type RunnerRegistry interface {
	RunnerForEnvironment(environmentID string) (runners.Runner, bool)
}

type StaticRunnerRegistry map[string]runners.Runner

func (r StaticRunnerRegistry) RunnerForEnvironment(environmentID string) (runners.Runner, bool) {
	runner, ok := r[environmentID]
	return runner, ok
}

func Run(ctx context.Context, runID string, registry RunnerRegistry, commands []Command) (Report, error) {
	if runID == "" {
		return Report{}, fmt.Errorf("run id is required")
	}
	if registry == nil {
		return Report{}, fmt.Errorf("runner registry is required")
	}
	report := Report{RunID: runID, Results: make([]Result, 0, len(commands))}
	for _, command := range commands {
		if command.ID == "" {
			return Report{}, fmt.Errorf("verification command id is required")
		}
		result := runOne(ctx, registry, command)
		report.Results = append(report.Results, result)
	}
	return report, nil
}

func runOne(ctx context.Context, registry RunnerRegistry, command Command) Result {
	runner, ok := registry.RunnerForEnvironment(command.EnvironmentID)
	if !ok {
		failure := FailureEnvironment
		return Result{
			CommandID:        command.ID,
			EnvironmentID:    command.EnvironmentID,
			RequiredForMerge: command.RequiredForMerge,
			Status:           ResultError,
			FailureClass:     &failure,
			Message:          "runner not found for environment",
		}
	}
	commandResult, err := runner.RunCommand(ctx, runners.RunCommandRequest{
		EnvironmentID:     command.EnvironmentID,
		Runner:            command.Runner,
		CWD:               command.WorkingDir,
		Argv:              command.Argv,
		Timeout:           command.Timeout,
		NetworkPolicy:     command.NetworkPolicy,
		CaptureStdout:     true,
		CaptureStderr:     true,
		RedactionRequired: true,
		ShellInvocation:   command.Runner != "direct",
		CommandKind:       "verification",
	})
	result := Result{
		CommandID:        command.ID,
		EnvironmentID:    command.EnvironmentID,
		RequiredForMerge: command.RequiredForMerge,
		CommandResult:    commandResult,
	}
	if err != nil {
		failure := FailureEnvironment
		result.Status = ResultError
		result.FailureClass = &failure
		result.Message = err.Error()
		return result
	}
	switch commandResult.Status {
	case runners.CommandSucceeded:
		result.Status = ResultPassed
	case runners.CommandFailed:
		failure := FailureCurrentDiff
		result.Status = ResultFailed
		result.FailureClass = &failure
		result.Message = commandResult.Stderr
	case runners.CommandTimedOut, runners.CommandBlocked, runners.CommandCancelled:
		failure := FailureEnvironment
		result.Status = ResultError
		result.FailureClass = &failure
		result.Message = string(commandResult.Status)
	default:
		failure := FailureUnknown
		result.Status = ResultError
		result.FailureClass = &failure
		result.Message = "unexpected command status"
	}
	return result
}
