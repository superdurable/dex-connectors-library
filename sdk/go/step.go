// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connector

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/superdurable/dex/sdk-go/dex"
)

var groupIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type StepPresentation struct {
	GroupID     string
	GroupLabel  string
	Explanation string
}

type QueryStepOutput[IN, OUT any] struct {
	Input  IN               `json:"input"`
	Result QueryResult[OUT] `json:"result"`
}

type MutationStepOutput[IN, OUT any] struct {
	Input  IN                  `json:"input"`
	Result MutationResult[OUT] `json:"result"`
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

type PersistenceRequirements struct {
	Attributes []dex.AttributeDef
	Streams    []dex.StreamDef
}

type QueryStepConfig[STEP_IN, OP_IN, OUT any] struct {
	StepType            string
	Presentation        StepPresentation
	Operation           Query[OP_IN, OUT]
	Connection          ConnectionRef
	BuildInput          func(STEP_IN) (OP_IN, error)
	Branches            []BranchTarget[QueryStepOutput[STEP_IN, OUT]]
	ResultAttribute     *dex.Attribute[QueryResult[OUT]]
	ProgressStream      *dex.Stream[ProgressUpdate]
	TextStream          *dex.Stream[string]
	TextOptions         []dex.BufferedTextStreamOption
	StepOptionsOverride *dex.StepOptions
}

type MutationStepConfig[STEP_IN, OP_IN, OUT any] struct {
	StepType            string
	Presentation        StepPresentation
	Operation           Mutation[OP_IN, OUT]
	Connection          ConnectionRef
	BuildInput          func(STEP_IN) (OP_IN, error)
	Branches            []BranchTarget[MutationStepOutput[STEP_IN, OUT]]
	ResultAttribute     *dex.Attribute[MutationResult[OUT]]
	ProgressStream      *dex.Stream[ProgressUpdate]
	TextStream          *dex.Stream[string]
	TextOptions         []dex.BufferedTextStreamOption
	StepOptionsOverride *dex.StepOptions
}

type QueryStep[STEP_IN, OP_IN, OUT any] struct {
	dex.NoWaitFor[STEP_IN]
	stepType        string
	presentation    StepPresentation
	operation       Query[OP_IN, OUT]
	connection      ConnectionRef
	buildInput      func(STEP_IN) (OP_IN, error)
	branches        map[BranchID]dex.Step[QueryStepOutput[STEP_IN, OUT]]
	resultAttribute *dex.Attribute[QueryResult[OUT]]
	progressStream  *dex.Stream[ProgressUpdate]
	textStream      *dex.Stream[string]
	textOptions     []dex.BufferedTextStreamOption
	stepOptions     *dex.StepOptions
}

type MutationStep[STEP_IN, OP_IN, OUT any] struct {
	dex.NoWaitFor[STEP_IN]
	stepType        string
	presentation    StepPresentation
	operation       Mutation[OP_IN, OUT]
	connection      ConnectionRef
	buildInput      func(STEP_IN) (OP_IN, error)
	branches        map[BranchID]dex.Step[MutationStepOutput[STEP_IN, OUT]]
	resultAttribute *dex.Attribute[MutationResult[OUT]]
	progressStream  *dex.Stream[ProgressUpdate]
	textStream      *dex.Stream[string]
	textOptions     []dex.BufferedTextStreamOption
	stepOptions     *dex.StepOptions
}

func NewQueryStep[STEP_IN, OP_IN, OUT any](config QueryStepConfig[STEP_IN, OP_IN, OUT]) (QueryStep[STEP_IN, OP_IN, OUT], error) {
	if nilValue(config.Operation) {
		return QueryStep[STEP_IN, OP_IN, OUT]{}, fmt.Errorf("query operation is required")
	}
	definition := config.Operation.Definition()
	if err := definition.Validate(); err != nil {
		return QueryStep[STEP_IN, OP_IN, OUT]{}, fmt.Errorf("query definition: %w", err)
	}
	if err := validateFactoryConfig(config.StepType, config.Presentation, config.Connection, config.BuildInput != nil); err != nil {
		return QueryStep[STEP_IN, OP_IN, OUT]{}, err
	}
	branches, err := validateBranchTargets(definition.Branches, config.Branches)
	if err != nil {
		return QueryStep[STEP_IN, OP_IN, OUT]{}, err
	}
	if err := validateResources(definition.ResultAttribute, definition.Progress, attributeName(config.ResultAttribute), streamName(config.ProgressStream), streamName(config.TextStream)); err != nil {
		return QueryStep[STEP_IN, OP_IN, OUT]{}, err
	}
	options, err := stepOptions(definition.StepDefaults, config.StepOptionsOverride)
	if err != nil {
		return QueryStep[STEP_IN, OP_IN, OUT]{}, err
	}
	return QueryStep[STEP_IN, OP_IN, OUT]{
		stepType: config.StepType, presentation: config.Presentation, operation: config.Operation,
		connection: config.Connection, buildInput: config.BuildInput, branches: branches,
		resultAttribute: config.ResultAttribute, progressStream: config.ProgressStream,
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
	if err := validateFactoryConfig(config.StepType, config.Presentation, config.Connection, config.BuildInput != nil); err != nil {
		return MutationStep[STEP_IN, OP_IN, OUT]{}, err
	}
	branches, err := validateBranchTargets(definition.Branches, config.Branches)
	if err != nil {
		return MutationStep[STEP_IN, OP_IN, OUT]{}, err
	}
	if err := validateResources(definition.ResultAttribute, definition.Progress, attributeName(config.ResultAttribute), streamName(config.ProgressStream), streamName(config.TextStream)); err != nil {
		return MutationStep[STEP_IN, OP_IN, OUT]{}, err
	}
	options, err := stepOptions(definition.StepDefaults, config.StepOptionsOverride)
	if err != nil {
		return MutationStep[STEP_IN, OP_IN, OUT]{}, err
	}
	return MutationStep[STEP_IN, OP_IN, OUT]{
		stepType: config.StepType, presentation: config.Presentation, operation: config.Operation,
		connection: config.Connection, buildInput: config.BuildInput, branches: branches,
		resultAttribute: config.ResultAttribute, progressStream: config.ProgressStream,
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

func (step QueryStep[STEP_IN, OP_IN, OUT]) Presentation() StepPresentation { return step.presentation }

func (step QueryStep[STEP_IN, OP_IN, OUT]) PersistenceRequirements() PersistenceRequirements {
	return persistenceRequirements(step.resultAttribute, step.progressStream, step.textStream)
}

func (step QueryStep[STEP_IN, OP_IN, OUT]) Execute(ctx dex.Context, input STEP_IN) (*dex.StepDecision, error) {
	operationInput, err := step.buildInput(input)
	var result QueryResult[OUT]
	if err != nil {
		result = failedQuery[OUT](step.operation.Definition(), "BuildInput returned an error")
	} else {
		result, err = RunQuery(ctx, step.operation, step.connection, operationInput, step.runOptions()...)
		if err != nil {
			return nil, err
		}
	}
	if step.resultAttribute != nil {
		if err := step.resultAttribute.Set(ctx, result); err != nil {
			return nil, err
		}
	}
	target := step.branches[result.Branch]
	if target == nil {
		return nil, fmt.Errorf("query result selected unconfigured branch %q", result.Branch)
	}
	return dex.GoTo(target, QueryStepOutput[STEP_IN, OUT]{Input: input, Result: result}), nil
}

func (step QueryStep[STEP_IN, OP_IN, OUT]) runOptions() []RunOption {
	return factoryRunOptions(step.progressStream, step.textStream, step.textOptions)
}

func (step MutationStep[STEP_IN, OP_IN, OUT]) GetStepType() string { return step.stepType }

func (step MutationStep[STEP_IN, OP_IN, OUT]) GetStepOptions() *dex.StepOptions {
	return cloneStepOptions(step.stepOptions)
}

func (step MutationStep[STEP_IN, OP_IN, OUT]) Presentation() StepPresentation {
	return step.presentation
}

func (step MutationStep[STEP_IN, OP_IN, OUT]) PersistenceRequirements() PersistenceRequirements {
	return persistenceRequirements(step.resultAttribute, step.progressStream, step.textStream)
}

func (step MutationStep[STEP_IN, OP_IN, OUT]) Execute(ctx dex.Context, input STEP_IN) (*dex.StepDecision, error) {
	operationInput, err := step.buildInput(input)
	var result MutationResult[OUT]
	if err != nil {
		result = failedMutation[OUT](step.operation.Definition(), "BuildInput returned an error")
	} else {
		result, err = RunMutation(ctx, step.operation, step.connection, operationInput, step.runOptions()...)
		if err != nil {
			return nil, err
		}
	}
	if step.resultAttribute != nil {
		if err := step.resultAttribute.Set(ctx, result); err != nil {
			return nil, err
		}
	}
	target := step.branches[result.Branch]
	if target == nil {
		return nil, fmt.Errorf("mutation result selected unconfigured branch %q", result.Branch)
	}
	return dex.GoTo(target, MutationStepOutput[STEP_IN, OUT]{Input: input, Result: result}), nil
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

func validateFactoryConfig(stepType string, presentation StepPresentation, connection ConnectionRef, hasBuildInput bool) error {
	if strings.TrimSpace(stepType) == "" {
		return fmt.Errorf("stable Step type is required")
	}
	if !groupIDPattern.MatchString(presentation.GroupID) || strings.TrimSpace(presentation.GroupLabel) == "" || strings.TrimSpace(presentation.Explanation) == "" {
		return fmt.Errorf("Step group, group label, and explanation are required")
	}
	if err := connection.Validate(); err != nil {
		return fmt.Errorf("Step connection: %w", err)
	}
	if !hasBuildInput {
		return fmt.Errorf("BuildInput is required")
	}
	return nil
}

func validateBranchTargets[T any](definitions []BranchDefinition, targets []BranchTarget[T]) (map[BranchID]dex.Step[T], error) {
	expected := make(map[BranchID]bool, len(definitions))
	for _, definition := range definitions {
		expected[definition.ID] = true
	}
	resolved := make(map[BranchID]dex.Step[T], len(targets))
	for _, target := range targets {
		if !expected[target.branch] {
			return nil, fmt.Errorf("branch target %q is not declared by the operation", target.branch)
		}
		if resolved[target.branch] != nil {
			return nil, fmt.Errorf("branch target %q is duplicated", target.branch)
		}
		if nilValue(target.target) || strings.TrimSpace(dex.GetFinalStepType(target.target)) == "" {
			return nil, fmt.Errorf("branch target %q must name a Step", target.branch)
		}
		resolved[target.branch] = target.target
	}
	for branch := range expected {
		if resolved[branch] == nil {
			return nil, fmt.Errorf("branch target %q is required", branch)
		}
	}
	return resolved, nil
}

func validateResources(requirement Requirement, progress ProgressCapabilities, attributeName, progressName, textName string) error {
	switch requirement {
	case RequirementNone:
		if attributeName != "" {
			return fmt.Errorf("operation forbids a Result Attribute")
		}
	case RequirementRequired:
		if attributeName == "" {
			return fmt.Errorf("operation requires a named Result Attribute")
		}
	case RequirementOptional:
	}
	if progressName != "" && !progress.Structured {
		return fmt.Errorf("operation does not support structured progress")
	}
	if textName != "" && !progress.Text {
		return fmt.Errorf("operation does not support text progress")
	}
	return nil
}

func attributeName[T any](attribute *dex.Attribute[T]) string {
	if attribute == nil {
		return ""
	}
	return attribute.AttributeName()
}

func streamName[T any](stream *dex.Stream[T]) string {
	if stream == nil {
		return ""
	}
	return stream.StreamName()
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

func persistenceRequirements[T any](attribute *dex.Attribute[T], progress *dex.Stream[ProgressUpdate], text *dex.Stream[string]) PersistenceRequirements {
	var requirements PersistenceRequirements
	if attribute != nil {
		requirements.Attributes = append(requirements.Attributes, *attribute)
	}
	if progress != nil {
		requirements.Streams = append(requirements.Streams, *progress)
	}
	if text != nil {
		requirements.Streams = append(requirements.Streams, *text)
	}
	return requirements
}
