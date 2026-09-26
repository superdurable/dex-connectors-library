// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestWrongFactoryTargetInputDoesNotCompile(t *testing.T) {
	command := exec.Command("go", "test", "./testdata/compile-fail-wrong-target")
	output, err := command.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "cannot use")
}

func TestTypedTargetBindsGeneratedBranch(t *testing.T) {
	target := sdkgo.GoTo(factoryTarget{})
	operation := &factoryQuery{definition: queryDefinition(testQueryRef)}
	step, err := sdkgo.NewQueryStep(sdkgo.QueryStepConfig[string, string, string]{
		StepType:            "GeneratedFactoryTarget",
		Annotations:         sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:           operation,
		Connection:          testConnection,
		MapToOperationInput: func(input string) string { return input },
		Branches: []sdkgo.BranchTarget[sdkgo.QueryResult[string]]{
			target.BranchTarget(testQuerySucceeded),
			sdkgo.GoTo(factoryTarget{}).BranchTarget(testQueryFailed),
			sdkgo.GoTo(factoryTarget{}).BranchTarget(testQueryDefect),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "GeneratedFactoryTarget", step.GetStepType())
}

type factoryQuery struct {
	definition sdkgo.QueryDefinition
	calls      int
	selected   sdkgo.BranchID
}

func (operation *factoryQuery) Definition() sdkgo.QueryDefinition { return operation.definition }

func (operation *factoryQuery) Invoke(sdkgo.Call, string) sdkgo.QueryAttempt[string] {
	operation.calls++
	selected := operation.selected
	if selected == "" {
		selected = testQuerySucceeded
	}
	return sdkgo.NewQueryBranch(selected, "value", nil, sdkgo.Receipt{})
}

type factoryTarget struct {
	dex.StepDefaultsNoWaitFor[sdkgo.QueryResult[string]]
}

func (factoryTarget) Execute(dex.Context, sdkgo.QueryResult[string]) (*dex.StepDecision, error) {
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
	base := sdkgo.QueryStepConfig[string, string, string]{
		StepType:    "FactoryQuery",
		Annotations: sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:   operation, Connection: testConnection,
		MapToOperationInput: func(input string) string { return input },
	}
	_, err := sdkgo.NewQueryStep(base)
	require.ErrorContains(t, err, "branch target")

	base.Branches = queryFactoryTargets()
	base.Branches = append(base.Branches, sdkgo.GoToBranch(testQuerySucceeded, factoryTarget{}))
	_, err = sdkgo.NewQueryStep(base)
	require.ErrorContains(t, err, "duplicated")

	base.Branches = append(queryFactoryTargets(), sdkgo.GoToBranch(sdkgo.BranchID("unknown"), factoryTarget{}))
	_, err = sdkgo.NewQueryStep(base)
	require.ErrorContains(t, err, "not declared")
}

func TestFactoryMapsOperationInputBeforeInvokingProvider(t *testing.T) {
	operation := &factoryQuery{definition: queryDefinition(testQueryRef)}
	mapperCalls := 0
	step := sdkgo.MustNewQueryStep(sdkgo.QueryStepConfig[string, string, string]{
		StepType:    "FactoryInputMapper",
		Annotations: sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:   operation, Connection: testConnection,
		MapToOperationInput: func(input string) string {
			mapperCalls++
			return "mapped-" + input
		},
		Branches: queryFactoryTargets(),
	})
	decision, err := step.Execute(testsupport.NewDexContext("flow-1", "step-1"), "input")
	require.NoError(t, err)
	require.NotNil(t, decision)
	require.Equal(t, 1, mapperCalls)
	require.Equal(t, 1, operation.calls)
}

func TestFactoryUsesConfiguredResourcesAndOverlaysExecuteOptions(t *testing.T) {
	definition := queryDefinition(testQueryRef)
	definition.StepDefaults = sdkgo.StepDefaults{
		ExecuteMethodTimeout: 30 * time.Second,
		ExecuteRetry:         &dex.RetryPolicy{MaximumAttempts: 5},
		ExecuteDurability:    dex.StepDurabilitySync,
	}
	operation := &factoryQuery{definition: definition}
	attribute := dex.DefineAttribute[sdkgo.QueryResult[string]]("factory-result")
	progress := dex.DefineStream[sdkgo.ProgressUpdate]("factory-progress", 1024)
	text := dex.DefineStream[string]("factory-text", 1024)
	step, err := sdkgo.NewQueryStep(sdkgo.QueryStepConfig[string, string, string]{
		StepType:    "FactoryResources",
		Annotations: sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:   operation, Connection: testConnection,
		MapToOperationInput: func(input string) string { return input },
		Branches:            queryFactoryTargets(), ResultAttribute: &attribute, ProgressStream: &progress, TextStream: &text,
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

	_, err = sdkgo.NewQueryStep(sdkgo.QueryStepConfig[string, string, string]{
		StepType:    "MissingResources",
		Annotations: sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:   operation, Connection: testConnection,
		MapToOperationInput: func(input string) string { return input }, Branches: queryFactoryTargets(),
	})
	require.NoError(t, err)
}

func TestFactoryRejectsWaitForOptionsAndStepRefFailsClosed(t *testing.T) {
	operation := &factoryQuery{definition: queryDefinition(testQueryRef)}
	_, err := sdkgo.NewQueryStep(sdkgo.QueryStepConfig[string, string, string]{
		StepType:    "FactoryWaitOptions",
		Annotations: sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:   operation, Connection: testConnection,
		MapToOperationInput: func(input string) string { return input }, Branches: queryFactoryTargets(),
		StepOptionsOverride: &dex.StepOptions{WaitForMethodTimeout: time.Second},
	})
	require.ErrorContains(t, err, "rejects WaitFor options")

	reference := sdkgo.StepRef[string]("ReferencedStep")
	decision, err := reference.Execute(testsupport.NewDexContext("flow-1", "step-1"), "input")
	require.Nil(t, decision)
	require.ErrorContains(t, err, "cannot execute")
	_, err = dex.NewRegistry([]dex.Flow{stepRefRegistrationFlow{reference: reference}})
	require.Error(t, err)
}

func TestOptionalBranchMayBeOmittedAndForceFailsWhenSelected(t *testing.T) {
	definition := queryDefinition(testQueryRef)
	for index := range definition.Branches {
		if definition.Branches[index].ID == testQueryDefect {
			definition.Branches[index].Optional = true
		}
	}
	operation := &factoryQuery{definition: definition, selected: testQueryDefect}
	step, err := sdkgo.NewQueryStep(sdkgo.QueryStepConfig[string, string, string]{
		StepType:    "OptionalDefect",
		Annotations: sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:   operation, Connection: testConnection,
		MapToOperationInput: func(input string) string { return input },
		Branches: []sdkgo.BranchTarget[sdkgo.QueryResult[string]]{
			sdkgo.GoToBranch(testQuerySucceeded, factoryTarget{}),
			sdkgo.GoToBranch(testQueryFailed, factoryTarget{}),
		},
	})
	require.NoError(t, err)
	decision, err := step.Execute(testsupport.NewDexContext("flow-1", "step-1"), "input")
	require.NoError(t, err)
	requireForceFail(t, decision, "defect")

	required := queryDefinition(testQueryRef)
	_, err = sdkgo.NewQueryStep(sdkgo.QueryStepConfig[string, string, string]{
		StepType:    "MissingRequired",
		Annotations: sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:   &factoryQuery{definition: required}, Connection: testConnection,
		MapToOperationInput: func(input string) string { return input },
		Branches: []sdkgo.BranchTarget[sdkgo.QueryResult[string]]{
			sdkgo.GoToBranch(testQuerySucceeded, factoryTarget{}),
			sdkgo.GoToBranch(testQueryDefect, factoryTarget{}),
		},
	})
	require.ErrorContains(t, err, "branch target \"failed\" is required")

	emptyOptional := queryDefinition(testQueryRef)
	for index := range emptyOptional.Branches {
		if emptyOptional.Branches[index].ID == testQueryDefect {
			emptyOptional.Branches[index].Optional = true
		}
	}
	_, err = sdkgo.NewQueryStep(sdkgo.QueryStepConfig[string, string, string]{
		StepType:    "EmptyOptional",
		Annotations: sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:   &factoryQuery{definition: emptyOptional}, Connection: testConnection,
		MapToOperationInput: func(input string) string { return input },
		Branches: []sdkgo.BranchTarget[sdkgo.QueryResult[string]]{
			sdkgo.GoToBranch(testQuerySucceeded, factoryTarget{}),
			sdkgo.GoToBranch(testQueryFailed, factoryTarget{}),
			sdkgo.Target[sdkgo.QueryResult[string]]{}.BranchTarget(testQueryDefect),
		},
	})
	require.NoError(t, err)
}

func TestUndeclaredBranchStillFails(t *testing.T) {
	definition := queryDefinition(testQueryRef)
	for index := range definition.Branches {
		if definition.Branches[index].ID == testQueryDefect {
			definition.Branches[index].Optional = true
		}
	}
	operation := &factoryQuery{definition: definition, selected: sdkgo.BranchID("unknown")}
	step := sdkgo.MustNewQueryStep(sdkgo.QueryStepConfig[string, string, string]{
		StepType:    "UnknownBranch",
		Annotations: sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "test query"},
		Operation:   operation, Connection: testConnection,
		MapToOperationInput: func(input string) string { return input },
		Branches: []sdkgo.BranchTarget[sdkgo.QueryResult[string]]{
			sdkgo.GoToBranch(testQuerySucceeded, factoryTarget{}),
			sdkgo.GoToBranch(testQueryFailed, factoryTarget{}),
		},
	})
	decision, err := step.Execute(testsupport.NewDexContext("flow-1", "step-1"), "input")
	require.NoError(t, err)
	requireForceFail(t, decision, "defect")
}

func requireForceFail(t *testing.T, decision *dex.StepDecision, branch string) {
	t.Helper()
	require.NotNil(t, decision)
	closeValue := reflect.ValueOf(decision).Elem().FieldByName("close")
	require.True(t, closeValue.IsValid())
	kind := closeValue.FieldByName("kind")
	reason := closeValue.FieldByName("reason")
	require.True(t, kind.IsValid())
	require.Equal(t, uint64(3), kind.Uint())
	require.Contains(t, reason.String(), branch)
}

func queryFactoryTargets() []sdkgo.BranchTarget[sdkgo.QueryResult[string]] {
	return []sdkgo.BranchTarget[sdkgo.QueryResult[string]]{
		sdkgo.GoToBranch(testQuerySucceeded, factoryTarget{}),
		sdkgo.GoToBranch(testQueryFailed, factoryTarget{}),
		sdkgo.GoToBranch(testQueryDefect, factoryTarget{}),
	}
}
