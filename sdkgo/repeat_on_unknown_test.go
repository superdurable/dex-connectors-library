// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex/sdk-go/dex"
)

type tieredMutation struct {
	definition sdkgo.MutationDefinition
	call       sdkgo.Call
}

func (operation *tieredMutation) Definition() sdkgo.MutationDefinition { return operation.definition }

func (*tieredMutation) IdempotencyKey(sdkgo.CallID, string) sdkgo.IdempotencyKey { return "" }

func (operation *tieredMutation) Invoke(call sdkgo.Call, _ string) sdkgo.MutationAttempt[string] {
	operation.call = call
	return sdkgo.NewMutationBranch(testMutationSucceeded, "ok", nil, sdkgo.Receipt{})
}

func tieredDefinition(repeatOnUnknown sdkgo.RepeatOnUnknown, maxSends int32) sdkgo.MutationDefinition {
	definition := mutationDefinition(testMutationRef)
	definition.RepeatOnUnknown = repeatOnUnknown
	definition.MaxSends = maxSends
	return definition
}

func tieredMutationStepConfig(operation *tieredMutation, maxSends int32) sdkgo.MutationStepConfig[string, string, string] {
	return sdkgo.MutationStepConfig[string, string, string]{
		StepType: "TieredMutation", Annotations: sdkgo.StepAnnotations{GroupID: "test", GroupLabel: "Test", Explanation: "test mutation"},
		Operation: operation, Connection: testConnection,
		MapToOperationInput: func(input string) string { return input },
		Branches: []sdkgo.BranchTarget[sdkgo.MutationResult[string]]{
			sdkgo.GoToBranch(testMutationSucceeded, factoryMutationTarget{}),
			sdkgo.GoToBranch(testMutationRejected, factoryMutationTarget{}),
			sdkgo.GoToBranch(testMutationUncertain, factoryMutationTarget{}),
			sdkgo.GoToBranch(testMutationDefect, factoryMutationTarget{}),
		},
		MaxSends: maxSends,
	}
}

func TestMutationDefinitionValidatesRepeatOnUnknown(t *testing.T) {
	for _, scenario := range []struct {
		repeatOnUnknown sdkgo.RepeatOnUnknown
		maxSends        int32
		isValid         bool
	}{
		{repeatOnUnknown: "", maxSends: 0, isValid: true},
		{repeatOnUnknown: sdkgo.RepeatOnUnknownSafe, isValid: true},
		{repeatOnUnknown: sdkgo.RepeatOnUnknownUnsafe, isValid: true},
		{repeatOnUnknown: sdkgo.RepeatOnUnknownLimited, maxSends: 2, isValid: true},
		{repeatOnUnknown: sdkgo.RepeatOnUnknownLimited, maxSends: sdkgo.MaxSendsCeiling, isValid: true},
		{repeatOnUnknown: sdkgo.RepeatOnUnknownLimited, maxSends: 1},
		{repeatOnUnknown: sdkgo.RepeatOnUnknownLimited, maxSends: 0},
		{repeatOnUnknown: sdkgo.RepeatOnUnknownLimited, maxSends: sdkgo.MaxSendsCeiling + 1},
		{repeatOnUnknown: sdkgo.RepeatOnUnknownSafe, maxSends: 3},
		{repeatOnUnknown: sdkgo.RepeatOnUnknownUnsafe, maxSends: 3},
		{repeatOnUnknown: "", maxSends: 3},
		{repeatOnUnknown: "sometimes"},
	} {
		err := tieredDefinition(scenario.repeatOnUnknown, scenario.maxSends).Validate()
		if scenario.isValid {
			require.NoError(t, err, "%+v", scenario)
		} else {
			require.Error(t, err, "%+v", scenario)
		}
	}
}

func TestCallMaxSendsFollowsDefinitionAndOverride(t *testing.T) {
	require.Equal(t, int32(1), sdkgo.Call{}.MaxSends())
	for _, scenario := range []struct {
		name       string
		definition sdkgo.MutationDefinition
		options    []sdkgo.RunOption
		want       int32
		wantDefect bool
	}{
		{name: "definition before tiers", definition: tieredDefinition("", 0), want: 1},
		{name: "safe", definition: tieredDefinition(sdkgo.RepeatOnUnknownSafe, 0), want: 1},
		{name: "unsafe", definition: tieredDefinition(sdkgo.RepeatOnUnknownUnsafe, 0), want: 1},
		{name: "limited", definition: tieredDefinition(sdkgo.RepeatOnUnknownLimited, 3), want: 3},
		{name: "raised override", definition: tieredDefinition(sdkgo.RepeatOnUnknownLimited, 3), options: []sdkgo.RunOption{sdkgo.WithMaxSends(5)}, want: 5},
		{name: "override that disables resend", definition: tieredDefinition(sdkgo.RepeatOnUnknownLimited, 3), options: []sdkgo.RunOption{sdkgo.WithMaxSends(1)}, want: 1},
		{name: "override of a safe operation", definition: tieredDefinition(sdkgo.RepeatOnUnknownSafe, 0), options: []sdkgo.RunOption{sdkgo.WithMaxSends(2)}, wantDefect: true},
		{name: "override of an unsafe operation", definition: tieredDefinition(sdkgo.RepeatOnUnknownUnsafe, 0), options: []sdkgo.RunOption{sdkgo.WithMaxSends(2)}, wantDefect: true},
		{name: "override of a definition before tiers", definition: tieredDefinition("", 0), options: []sdkgo.RunOption{sdkgo.WithMaxSends(2)}, wantDefect: true},
		{name: "zero override", definition: tieredDefinition(sdkgo.RepeatOnUnknownLimited, 3), options: []sdkgo.RunOption{sdkgo.WithMaxSends(0)}, wantDefect: true},
		{name: "override above the ceiling", definition: tieredDefinition(sdkgo.RepeatOnUnknownLimited, 3), options: []sdkgo.RunOption{sdkgo.WithMaxSends(sdkgo.MaxSendsCeiling + 1)}, wantDefect: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			operation := &tieredMutation{definition: scenario.definition}
			result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "step-1"), operation, testConnection, "input", scenario.options...)
			require.NoError(t, err)
			if scenario.wantDefect {
				require.Equal(t, sdkgo.DefectBranchID, result.Branch)
				require.Zero(t, operation.call, "the operation is not invoked")
				return
			}
			require.Equal(t, testMutationSucceeded, result.Branch)
			require.Equal(t, scenario.want, operation.call.MaxSends())
		})
	}

	query := &queryOperation{}
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("flow-1", "step-1"), query, testConnection, "input")
	require.NoError(t, err)
	require.Equal(t, testQuerySucceeded, result.Branch)
	require.Equal(t, int32(1), query.call.MaxSends())
	result, err = sdkgo.RunQuery(testsupport.NewDexContext("flow-1", "step-1"), &queryOperation{}, testConnection, "input", sdkgo.WithMaxSends(2))
	require.NoError(t, err)
	require.Equal(t, sdkgo.DefectBranchID, result.Branch)
}

func TestMutationStepMaxSendsOverride(t *testing.T) {
	for _, definition := range []sdkgo.MutationDefinition{
		tieredDefinition("", 0), tieredDefinition(sdkgo.RepeatOnUnknownSafe, 0), tieredDefinition(sdkgo.RepeatOnUnknownUnsafe, 0),
	} {
		_, err := sdkgo.NewMutationStep(tieredMutationStepConfig(&tieredMutation{definition: definition}, 2))
		require.ErrorContains(t, err, "RepeatOnUnknown is limited", definition.RepeatOnUnknown)
	}
	for _, maxSends := range []int32{-1, sdkgo.MaxSendsCeiling + 1} {
		_, err := sdkgo.NewMutationStep(tieredMutationStepConfig(&tieredMutation{definition: tieredDefinition(sdkgo.RepeatOnUnknownLimited, 3)}, maxSends))
		require.ErrorContains(t, err, "max sends override", maxSends)
	}

	operation := &tieredMutation{definition: tieredDefinition(sdkgo.RepeatOnUnknownLimited, 3)}
	step := sdkgo.MustNewMutationStep(tieredMutationStepConfig(operation, 5))
	_, err := step.Execute(testsupport.NewDexContext("flow-1", "step-1"), "input")
	require.NoError(t, err)
	require.Equal(t, int32(5), operation.call.MaxSends(), "the Step carries its cap into every attempt's Call")

	step = sdkgo.MustNewMutationStep(tieredMutationStepConfig(operation, 0))
	_, err = step.Execute(testsupport.NewDexContext("flow-1", "step-2"), "input")
	require.NoError(t, err)
	require.Equal(t, int32(3), operation.call.MaxSends())
}

func TestMutationStepMaxSendsOverrideWidensLimitedAttempts(t *testing.T) {
	limitedPolicy := &dex.RetryPolicy{InitialInterval: time.Second, BackoffCoefficient: 2, MaximumAttempts: 5, TotalDuration: 10 * time.Minute}
	for _, scenario := range []struct {
		name         string
		policy       *dex.RetryPolicy
		maxSends     int32
		override     *dex.RetryPolicy
		wantAttempts int32
	}{
		{name: "raised cap adds its difference", policy: limitedPolicy, maxSends: 5, wantAttempts: 7},
		{name: "ceiling", policy: limitedPolicy, maxSends: sdkgo.MaxSendsCeiling, wantAttempts: 12},
		{name: "unchanged cap", policy: limitedPolicy, maxSends: 3, wantAttempts: 5},
		{name: "lowered cap keeps attempts", policy: limitedPolicy, maxSends: 1, wantAttempts: 5},
		{name: "no override", policy: limitedPolicy, maxSends: 0, wantAttempts: 5},
		{name: "unlimited attempts stay unlimited", policy: &dex.RetryPolicy{MaximumAttempts: 0, TotalDuration: time.Hour}, maxSends: 5, wantAttempts: 0},
		{name: "omitted policy uses the unlimited default", maxSends: 5, wantAttempts: 0},
		{name: "application policy wins", policy: limitedPolicy, maxSends: 5, override: &dex.RetryPolicy{MaximumAttempts: 4, TotalDuration: time.Hour}, wantAttempts: 4},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			definition := tieredDefinition(sdkgo.RepeatOnUnknownLimited, 3)
			definition.StepDefaults.ExecuteRetry = scenario.policy
			config := tieredMutationStepConfig(&tieredMutation{definition: definition}, scenario.maxSends)
			if scenario.override != nil {
				config.StepOptionsOverride = &dex.StepOptions{ExecuteRetry: scenario.override}
			}
			step := sdkgo.MustNewMutationStep(config)
			require.Equal(t, scenario.wantAttempts, step.GetStepOptions().ExecuteRetry.MaximumAttempts)
			require.Equal(t, int32(5), limitedPolicy.MaximumAttempts, "the definition policy is not mutated")
		})
	}
}

func TestMutationDefinitionJSONOmitsAbsentTier(t *testing.T) {
	encoded, err := json.Marshal(tieredDefinition("", 0))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "repeatOnUnknown")
	require.NotContains(t, string(encoded), "maxSends")
	encoded, err = json.Marshal(tieredDefinition(sdkgo.RepeatOnUnknownLimited, 3))
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"repeatOnUnknown":"limited","maxSends":3`)
}
