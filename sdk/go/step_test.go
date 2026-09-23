// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connector_test

import (
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex-connectors-library/sdk/go/internal/testsupport"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestWrongFactoryTargetInputDoesNotCompile(t *testing.T) {
	command := exec.Command("go", "test", "./testdata/compile-fail-wrong-target")
	output, err := command.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "cannot use")
}

func TestTypedTargetBindsGeneratedBranch(t *testing.T) {
	target := connector.GoTo(factoryTarget{})
	operation := &factoryQuery{definition: queryDefinition(testQueryRef)}
	step, err := connector.NewQueryStep(connector.QueryStepConfig[string, string, string]{
		StepType:     "GeneratedFactoryTarget",
		Presentation: connector.StepPresentation{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:    operation,
		Connection:   testConnection,
		BuildInput:   func(input string) (string, error) { return input, nil },
		Branches: []connector.BranchTarget[connector.QueryStepOutput[string, string]]{
			target.BranchTarget(testQuerySucceeded),
			connector.GoTo(factoryTarget{}).BranchTarget(testQueryFailed),
			connector.GoTo(factoryTarget{}).BranchTarget(testQueryDefect),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "GeneratedFactoryTarget", step.GetStepType())
}

type factoryQuery struct {
	definition connector.QueryDefinition
	calls      int
}

func (operation *factoryQuery) Definition() connector.QueryDefinition { return operation.definition }

func (operation *factoryQuery) Invoke(connector.Call, string) connector.QueryAttempt[string] {
	operation.calls++
	return connector.NewQueryBranch(testQuerySucceeded, "value", nil, connector.Receipt{})
}

type factoryTarget struct {
	dex.StepDefaultsNoWaitFor[connector.QueryStepOutput[string, string]]
}

func (factoryTarget) Execute(dex.Context, connector.QueryStepOutput[string, string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type factoryFailureTarget struct {
	dex.StepDefaultsNoWaitFor[string]
}

type stepRefRegistrationFlow struct {
	dex.FlowDefaults
	reference dex.Step[string]
}

func (flow stepRefRegistrationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(flow.reference)}
}

func (stepRefRegistrationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

func (factoryFailureTarget) Execute(dex.Context, string) (*dex.StepDecision, error) {
	return dex.ForceFail("execute exhausted"), nil
}

func TestQueryFactoryRequiresExactlyOneTargetPerBranch(t *testing.T) {
	operation := &factoryQuery{definition: queryDefinition(testQueryRef)}
	base := connector.QueryStepConfig[string, string, string]{
		StepType:     "FactoryQuery",
		Presentation: connector.StepPresentation{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:    operation, Connection: testConnection, BuildInput: func(input string) (string, error) { return input, nil },
	}
	_, err := connector.NewQueryStep(base)
	require.ErrorContains(t, err, "branch target")

	base.Branches = queryFactoryTargets()
	base.Branches = append(base.Branches, connector.GoToBranch(testQuerySucceeded, factoryTarget{}))
	_, err = connector.NewQueryStep(base)
	require.ErrorContains(t, err, "duplicated")

	base.Branches = append(queryFactoryTargets(), connector.GoToBranch(connector.BranchID("unknown"), factoryTarget{}))
	_, err = connector.NewQueryStep(base)
	require.ErrorContains(t, err, "not declared")
}

func TestFactoryBuildInputDefectDoesNotInvokeProvider(t *testing.T) {
	operation := &factoryQuery{definition: queryDefinition(testQueryRef)}
	step := connector.MustNewQueryStep(connector.QueryStepConfig[string, string, string]{
		StepType:     "FactoryBuildDefect",
		Presentation: connector.StepPresentation{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:    operation, Connection: testConnection,
		BuildInput: func(string) (string, error) { return "", errors.New("bad input") },
		Branches:   queryFactoryTargets(),
	})
	decision, err := step.Execute(testsupport.NewDexContext("flow-1", "step-1"), "input")
	require.NoError(t, err)
	require.NotNil(t, decision)
	require.Zero(t, operation.calls)
}

func TestFactoryValidatesResourcesAndOverlaysExecuteOptions(t *testing.T) {
	definition := queryDefinition(testQueryRef)
	definition.ResultAttribute = connector.RequirementRequired
	definition.Progress = connector.ProgressCapabilities{Structured: true, Text: true}
	definition.StepDefaults = connector.StepDefaults{
		ExecuteMethodTimeout: 30 * time.Second,
		ExecuteRetry:         &dex.RetryPolicy{MaximumAttempts: 5},
		ExecuteDurability:    dex.StepDurabilitySync,
	}
	operation := &factoryQuery{definition: definition}
	attribute := dex.DefineAttribute[connector.QueryResult[string]]("factory-result")
	progress := dex.DefineStream[connector.ProgressUpdate]("factory-progress", 1024)
	text := dex.DefineStream[string]("factory-text", 1024)
	step, err := connector.NewQueryStep(connector.QueryStepConfig[string, string, string]{
		StepType:     "FactoryResources",
		Presentation: connector.StepPresentation{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:    operation, Connection: testConnection,
		BuildInput: func(input string) (string, error) { return input, nil },
		Branches:   queryFactoryTargets(), ResultAttribute: &attribute, ProgressStream: &progress, TextStream: &text,
		StepOptionsOverride: &dex.StepOptions{
			ExecuteMethodTimeout: time.Minute,
			ExecuteFailure:       dex.ProceedToOnExecuteFailure(factoryFailureTarget{}, nil),
		},
	})
	require.NoError(t, err)
	require.Equal(t, time.Minute, step.GetStepOptions().ExecuteMethodTimeout)
	require.Equal(t, int32(5), step.GetStepOptions().ExecuteRetry.MaximumAttempts)
	require.Equal(t, dex.StepDurabilitySync, step.GetStepOptions().ExecuteDurability)
	require.NotNil(t, step.GetStepOptions().ExecuteFailure)
	require.Len(t, step.PersistenceRequirements().Attributes, 1)
	require.Len(t, step.PersistenceRequirements().Streams, 2)

	_, err = connector.NewQueryStep(connector.QueryStepConfig[string, string, string]{
		StepType:     "MissingResources",
		Presentation: connector.StepPresentation{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:    operation, Connection: testConnection,
		BuildInput: func(input string) (string, error) { return input, nil }, Branches: queryFactoryTargets(),
	})
	require.ErrorContains(t, err, "requires a named Result Attribute")
}

func TestFactoryRejectsWaitForOptionsAndStepRefFailsClosed(t *testing.T) {
	operation := &factoryQuery{definition: queryDefinition(testQueryRef)}
	_, err := connector.NewQueryStep(connector.QueryStepConfig[string, string, string]{
		StepType:     "FactoryWaitOptions",
		Presentation: connector.StepPresentation{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:    operation, Connection: testConnection,
		BuildInput: func(input string) (string, error) { return input, nil }, Branches: queryFactoryTargets(),
		StepOptionsOverride: &dex.StepOptions{WaitForMethodTimeout: time.Second},
	})
	require.ErrorContains(t, err, "rejects WaitFor options")

	reference := connector.StepRef[string]("ReferencedStep")
	decision, err := reference.Execute(testsupport.NewDexContext("flow-1", "step-1"), "input")
	require.Nil(t, decision)
	require.ErrorContains(t, err, "cannot execute")
	_, err = dex.NewRegistry([]dex.Flow{stepRefRegistrationFlow{reference: reference}})
	require.Error(t, err)
}

func queryFactoryTargets() []connector.BranchTarget[connector.QueryStepOutput[string, string]] {
	return []connector.BranchTarget[connector.QueryStepOutput[string, string]]{
		connector.GoToBranch(testQuerySucceeded, factoryTarget{}),
		connector.GoToBranch(testQueryFailed, factoryTarget{}),
		connector.GoToBranch(testQueryDefect, factoryTarget{}),
	}
}
