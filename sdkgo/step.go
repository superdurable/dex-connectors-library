// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/superdurable/dex/sdk-go/dex"
)

var groupIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// StepAnnotations describes how Dex tooling explains and groups a Connector Step.
// Applications provide stable group metadata when constructing an operation-specific Step.
type StepAnnotations struct {
	// GroupID identifies the Step group with a stable lowercase identifier.
	GroupID string
	// GroupLabel is the human-readable Step group label.
	GroupLabel string
	// Explanation is one sentence describing what the Step does.
	Explanation string
}

// QueryFactoryConfigMarker identifies generated Query Step factory configs.
type QueryFactoryConfigMarker struct{}

// MutationFactoryConfigMarker identifies generated Mutation Step factory configs.
type MutationFactoryConfigMarker struct{}

// Target is one typed destination supplied to an operation-specific factory.
type Target[T any] struct {
	target dex.Step[T]
}

// GoTo creates a typed target for an operation-specific factory branch.
func GoTo[T any](target dex.Step[T]) Target[T] {
	return Target[T]{target: target}
}

// HasStep reports whether the target names a Step.
func (target Target[T]) HasStep() bool {
	return targetHasStep(target.target)
}

// BranchTarget binds this target to a generated operation branch.
func (target Target[T]) BranchTarget(branch BranchID) BranchTarget[T] {
	return GoToBranch(branch, target.target)
}

type BranchTarget[T any] struct {
	branch BranchID
	target dex.Step[T]
}

// GoToBranch binds one operation branch to one typed Dex Step target.
func GoToBranch[T any](branch BranchID, target dex.Step[T]) BranchTarget[T] {
	return BranchTarget[T]{branch: branch, target: target}
}

type QueryStepConfig[STEP_IN, OP_IN, OUT any] struct {
	StepType            string
	Annotations         StepAnnotations
	Operation           Query[OP_IN, OUT]
	Connection          ConnectionRef
	MapToOperationInput func(STEP_IN) OP_IN
	Branches            []BranchTarget[QueryResult[OUT]]
	ResultAttribute     *dex.Attribute[QueryResult[OUT]]
	ProgressStream      *dex.Stream[ProgressUpdate]
	TextStream          *dex.Stream[string]
	TextOptions         []dex.BufferedTextStreamOption
	StepOptionsOverride *dex.StepOptions
}

type MutationStepConfig[STEP_IN, OP_IN, OUT any] struct {
	StepType            string
	Annotations         StepAnnotations
	Operation           Mutation[OP_IN, OUT]
	Connection          ConnectionRef
	MapToOperationInput func(STEP_IN) OP_IN
	Branches            []BranchTarget[MutationResult[OUT]]
	ResultAttribute     *dex.Attribute[MutationResult[OUT]]
	ProgressStream      *dex.Stream[ProgressUpdate]
	TextStream          *dex.Stream[string]
	TextOptions         []dex.BufferedTextStreamOption
	StepOptionsOverride *dex.StepOptions
}

type QueryStep[STEP_IN, OP_IN, OUT any] struct {
	dex.NoWaitFor[STEP_IN]
	stepType            string
	annotations         StepAnnotations
	operation           Query[OP_IN, OUT]
	connection          ConnectionRef
	mapToOperationInput func(STEP_IN) OP_IN
	branches            map[BranchID]dex.Step[QueryResult[OUT]]
	optionalBranches    map[BranchID]bool
	resultAttribute     *dex.Attribute[QueryResult[OUT]]
	progressStream      *dex.Stream[ProgressUpdate]
	textStream          *dex.Stream[string]
	textOptions         []dex.BufferedTextStreamOption
	stepOptions         *dex.StepOptions
}

type MutationStep[STEP_IN, OP_IN, OUT any] struct {
	dex.NoWaitFor[STEP_IN]
	stepType            string
	annotations         StepAnnotations
	operation           Mutation[OP_IN, OUT]
	connection          ConnectionRef
	mapToOperationInput func(STEP_IN) OP_IN
	branches            map[BranchID]dex.Step[MutationResult[OUT]]
	optionalBranches    map[BranchID]bool
	resultAttribute     *dex.Attribute[MutationResult[OUT]]
	progressStream      *dex.Stream[ProgressUpdate]
	textStream          *dex.Stream[string]
	textOptions         []dex.BufferedTextStreamOption
	stepOptions         *dex.StepOptions
}

func NewQueryStep[STEP_IN, OP_IN, OUT any](config QueryStepConfig[STEP_IN, OP_IN, OUT]) (QueryStep[STEP_IN, OP_IN, OUT], error) {
	if nilValue(config.Operation) {
		return QueryStep[STEP_IN, OP_IN, OUT]{}, fmt.Errorf("query operation is required")
	}
	definition := config.Operation.Definition()
	if err := definition.Validate(); err != nil {
		return QueryStep[STEP_IN, OP_IN, OUT]{}, fmt.Errorf("query definition: %w", err)
	}
	if err := validateFactoryConfig(config.StepType, config.Annotations, config.Connection, config.MapToOperationInput != nil); err != nil {
		return QueryStep[STEP_IN, OP_IN, OUT]{}, err
	}
	branches, optionalBranches, err := validateBranchTargets(definition.Branches, config.Branches)
	if err != nil {
		return QueryStep[STEP_IN, OP_IN, OUT]{}, err
	}
	options, err := stepOptions(definition.StepDefaults, config.StepOptionsOverride)
	if err != nil {
		return QueryStep[STEP_IN, OP_IN, OUT]{}, err
	}
	return QueryStep[STEP_IN, OP_IN, OUT]{
		stepType: config.StepType, annotations: config.Annotations, operation: config.Operation,
		connection: config.Connection, mapToOperationInput: config.MapToOperationInput, branches: branches,
		optionalBranches: optionalBranches,
		resultAttribute:  config.ResultAttribute, progressStream: config.ProgressStream,
		textStream: config.TextStream, textOptions: append([]dex.BufferedTextStreamOption(nil), config.TextOptions...),
		stepOptions: options,
	}, nil
}

func MustNewQueryStep[STEP_IN, OP_IN, OUT any](config QueryStepConfig[STEP_IN, OP_IN, OUT]) QueryStep[STEP_IN, OP_IN, OUT] {
	step, err := NewQueryStep(config)
	if err != nil {
		panic(err)
	}
	return step
}

func NewMutationStep[STEP_IN, OP_IN, OUT any](config MutationStepConfig[STEP_IN, OP_IN, OUT]) (MutationStep[STEP_IN, OP_IN, OUT], error) {
	if nilValue(config.Operation) {
		return MutationStep[STEP_IN, OP_IN, OUT]{}, fmt.Errorf("mutation operation is required")
	}
	definition := config.Operation.Definition()
	if err := definition.Validate(); err != nil {
		return MutationStep[STEP_IN, OP_IN, OUT]{}, fmt.Errorf("mutation definition: %w", err)
	}
	if err := validateFactoryConfig(config.StepType, config.Annotations, config.Connection, config.MapToOperationInput != nil); err != nil {
		return MutationStep[STEP_IN, OP_IN, OUT]{}, err
	}
	branches, optionalBranches, err := validateBranchTargets(definition.Branches, config.Branches)
	if err != nil {
		return MutationStep[STEP_IN, OP_IN, OUT]{}, err
	}
	options, err := stepOptions(definition.StepDefaults, config.StepOptionsOverride)
	if err != nil {
		return MutationStep[STEP_IN, OP_IN, OUT]{}, err
	}
	return MutationStep[STEP_IN, OP_IN, OUT]{
		stepType: config.StepType, annotations: config.Annotations, operation: config.Operation,
		connection: config.Connection, mapToOperationInput: config.MapToOperationInput, branches: branches,
		optionalBranches: optionalBranches,
		resultAttribute:  config.ResultAttribute, progressStream: config.ProgressStream,
		textStream: config.TextStream, textOptions: append([]dex.BufferedTextStreamOption(nil), config.TextOptions...),
		stepOptions: options,
	}, nil
}

func MustNewMutationStep[STEP_IN, OP_IN, OUT any](config MutationStepConfig[STEP_IN, OP_IN, OUT]) MutationStep[STEP_IN, OP_IN, OUT] {
	step, err := NewMutationStep(config)
	if err != nil {
		panic(err)
	}
	return step
}

func (step QueryStep[STEP_IN, OP_IN, OUT]) GetStepType() string { return step.stepType }

func (step QueryStep[STEP_IN, OP_IN, OUT]) GetStepOptions() *dex.StepOptions {
	return cloneStepOptions(step.stepOptions)
}

func (step QueryStep[STEP_IN, OP_IN, OUT]) Annotations() StepAnnotations { return step.annotations }

func (step QueryStep[STEP_IN, OP_IN, OUT]) Execute(ctx dex.Context, input STEP_IN) (*dex.StepDecision, error) {
	operationInput := step.mapToOperationInput(input)
	result, err := RunQuery(ctx, step.operation, step.connection, operationInput, step.runOptions()...)
	if err != nil {
		return nil, err
	}
	if step.resultAttribute != nil {
		if err := step.resultAttribute.Set(ctx, result); err != nil {
			return nil, err
		}
	}
	return routeBranch("query", result.Branch, result, step.branches, step.optionalBranches)
}

func (step QueryStep[STEP_IN, OP_IN, OUT]) runOptions() []RunOption {
	return factoryRunOptions(step.progressStream, step.textStream, step.textOptions)
}

func (step MutationStep[STEP_IN, OP_IN, OUT]) GetStepType() string { return step.stepType }

func (step MutationStep[STEP_IN, OP_IN, OUT]) GetStepOptions() *dex.StepOptions {
	return cloneStepOptions(step.stepOptions)
}

func (step MutationStep[STEP_IN, OP_IN, OUT]) Annotations() StepAnnotations {
	return step.annotations
}

func (step MutationStep[STEP_IN, OP_IN, OUT]) Execute(ctx dex.Context, input STEP_IN) (*dex.StepDecision, error) {
	operationInput := step.mapToOperationInput(input)
	result, err := RunMutation(ctx, step.operation, step.connection, operationInput, step.runOptions()...)
	if err != nil {
		return nil, err
	}
	if step.resultAttribute != nil {
		if err := step.resultAttribute.Set(ctx, result); err != nil {
			return nil, err
		}
	}
	return routeBranch("mutation", result.Branch, result, step.branches, step.optionalBranches)
}

func (step MutationStep[STEP_IN, OP_IN, OUT]) runOptions() []RunOption {
	return factoryRunOptions(step.progressStream, step.textStream, step.textOptions)
}

func StepRef[T any](stableStepType string) dex.Step[T] {
	return stepReference[T]{stepType: stableStepType}
}

type stepReference[T any] struct {
	dex.NoWaitFor[T]
	stepType string
}

func (stepReference[T]) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: -1}
}

func (reference stepReference[T]) GetStepType() string { return reference.stepType }

func (reference stepReference[T]) Execute(dex.Context, T) (*dex.StepDecision, error) {
	return nil, fmt.Errorf("connector StepRef %q cannot execute", reference.stepType)
}

func validateFactoryConfig(stepType string, annotations StepAnnotations, connection ConnectionRef, hasOperationInputMapper bool) error {
	if strings.TrimSpace(stepType) == "" {
		return fmt.Errorf("stable Step type is required")
	}
	if !groupIDPattern.MatchString(annotations.GroupID) || strings.TrimSpace(annotations.GroupLabel) == "" || strings.TrimSpace(annotations.Explanation) == "" {
		return fmt.Errorf("Step group, group label, and explanation are required")
	}
	if err := connection.Validate(); err != nil {
		return fmt.Errorf("Step connection: %w", err)
	}
	if !hasOperationInputMapper {
		return fmt.Errorf("MapToOperationInput is required")
	}
	return nil
}

func validateBranchTargets[T any](definitions []BranchDefinition, targets []BranchTarget[T]) (map[BranchID]dex.Step[T], map[BranchID]bool, error) {
	expected := make(map[BranchID]bool, len(definitions))
	optional := make(map[BranchID]bool)
	for _, definition := range definitions {
		expected[definition.ID] = true
		if definition.Optional {
			optional[definition.ID] = true
		}
	}
	resolved := make(map[BranchID]dex.Step[T], len(targets))
	for _, target := range targets {
		if !expected[target.branch] {
			return nil, nil, fmt.Errorf("branch target %q is not declared by the operation", target.branch)
		}
		if resolved[target.branch] != nil {
			return nil, nil, fmt.Errorf("branch target %q is duplicated", target.branch)
		}
		if !targetHasStep(target.target) {
			return nil, nil, fmt.Errorf("branch target %q must name a Step", target.branch)
		}
		resolved[target.branch] = target.target
	}
	for branch := range expected {
		if resolved[branch] == nil && !optional[branch] {
			return nil, nil, fmt.Errorf("branch target %q is required", branch)
		}
	}
	return resolved, optional, nil
}

func routeBranch[T any](kind string, branch BranchID, result T, branches map[BranchID]dex.Step[T], optional map[BranchID]bool) (*dex.StepDecision, error) {
	if target := branches[branch]; target != nil {
		return dex.GoTo(target, result), nil
	}
	if optional[branch] {
		return dex.ForceFail(fmt.Sprintf("connector branch %q has no target", branch)), nil
	}
	return nil, fmt.Errorf("%s result selected unconfigured branch %q", kind, branch)
}

func targetHasStep[T any](target dex.Step[T]) bool {
	return !nilValue(target) && strings.TrimSpace(dex.GetFinalStepType(target)) != ""
}

func stepOptions(defaults StepDefaults, override *dex.StepOptions) (*dex.StepOptions, error) {
	if override != nil && hasWaitForOptions(override) {
		return nil, fmt.Errorf("execute-only connector Step rejects WaitFor options")
	}
	merged := &dex.StepOptions{
		ExecuteMethodTimeout: defaults.ExecuteMethodTimeout,
		HeartbeatTimeout:     defaults.HeartbeatTimeout,
		ExecuteRetry:         cloneRetryPolicy(defaults.ExecuteRetry),
		ExecuteDurability:    defaults.ExecuteDurability,
	}
	if override != nil {
		if override.ExecuteMethodTimeout != 0 {
			merged.ExecuteMethodTimeout = override.ExecuteMethodTimeout
		}
		if override.HeartbeatTimeout != 0 {
			merged.HeartbeatTimeout = override.HeartbeatTimeout
		}
		if override.ExecuteRetry != nil {
			merged.ExecuteRetry = cloneRetryPolicy(override.ExecuteRetry)
		}
		if override.ExecuteFailure != nil {
			merged.ExecuteFailure = override.ExecuteFailure
		}
		if override.ExecuteDurability != dex.StepDurabilityDefault {
			merged.ExecuteDurability = override.ExecuteDurability
		}
		if override.ExecuteLockAttributes != nil {
			merged.ExecuteLockAttributes = append([]dex.AttributeLock(nil), override.ExecuteLockAttributes...)
		}
		if override.ExecuteLoadAttributeMaps != nil {
			merged.ExecuteLoadAttributeMaps = append([]dex.AttributeDef(nil), override.ExecuteLoadAttributeMaps...)
		}
		if override.ExecuteLoadAttributeMapInstances != nil {
			merged.ExecuteLoadAttributeMapInstances = append([]dex.AttributeMapLoad(nil), override.ExecuteLoadAttributeMapInstances...)
		}
		if override.ExecuteLoadChannels != nil {
			merged.ExecuteLoadChannels = append([]dex.ChannelDef(nil), override.ExecuteLoadChannels...)
		}
		if override.ExecuteLoadChannelMaps != nil {
			merged.ExecuteLoadChannelMaps = append([]dex.ChannelDef(nil), override.ExecuteLoadChannelMaps...)
		}
		if override.ExecuteLoadChannelMapInstances != nil {
			merged.ExecuteLoadChannelMapInstances = append([]dex.ChannelMapLoad(nil), override.ExecuteLoadChannelMapInstances...)
		}
	}
	return merged, nil
}

func hasWaitForOptions(options *dex.StepOptions) bool {
	return options.WaitForMethodTimeout != 0 || options.WaitForRetry != nil ||
		options.WaitForFailure != dex.FailFlowOnWaitForFailure || options.WaitForDurability != dex.StepDurabilityDefault ||
		options.WaitForLockAttributes != nil || options.WaitForLoadAttributeMaps != nil ||
		options.WaitForLoadAttributeMapInstances != nil || options.WaitForLoadChannels != nil ||
		options.WaitForLoadChannelMaps != nil || options.WaitForLoadChannelMapInstances != nil
}

func cloneRetryPolicy(policy *dex.RetryPolicy) *dex.RetryPolicy {
	if policy == nil {
		return nil
	}
	copy := *policy
	return &copy
}

func cloneStepOptions(options *dex.StepOptions) *dex.StepOptions {
	if options == nil {
		return nil
	}
	clone := *options
	clone.ExecuteRetry = cloneRetryPolicy(options.ExecuteRetry)
	clone.ExecuteLockAttributes = append([]dex.AttributeLock(nil), options.ExecuteLockAttributes...)
	clone.ExecuteLoadAttributeMaps = append([]dex.AttributeDef(nil), options.ExecuteLoadAttributeMaps...)
	clone.ExecuteLoadAttributeMapInstances = append([]dex.AttributeMapLoad(nil), options.ExecuteLoadAttributeMapInstances...)
	clone.ExecuteLoadChannels = append([]dex.ChannelDef(nil), options.ExecuteLoadChannels...)
	clone.ExecuteLoadChannelMaps = append([]dex.ChannelDef(nil), options.ExecuteLoadChannelMaps...)
	clone.ExecuteLoadChannelMapInstances = append([]dex.ChannelMapLoad(nil), options.ExecuteLoadChannelMapInstances...)
	return &clone
}

func nilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func factoryRunOptions(progress *dex.Stream[ProgressUpdate], text *dex.Stream[string], textOptions []dex.BufferedTextStreamOption) []RunOption {
	options := make([]RunOption, 0, 2)
	if progress != nil {
		options = append(options, WithProgressStream(*progress))
	}
	if text != nil {
		options = append(options, WithTextStream(*text, textOptions...))
	}
	return options
}
