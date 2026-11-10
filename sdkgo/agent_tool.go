// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

var agentToolOperationNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// AgentToolEffect is how an agent host may repeat a tool call. The values are
// the Dex MCP effect conventions, so a host can copy them into tool metadata.
type AgentToolEffect string

const (
	// AgentToolEffectRead marks a Query: repeating it changes nothing.
	AgentToolEffectRead AgentToolEffect = "read"
	// AgentToolEffectIdempotentWrite marks a Mutation whose manifest sets
	// agent.retry: safe, so a repeat with the same Call ID inside the
	// operation's Execute retry window cannot make a second effect.
	AgentToolEffectIdempotentWrite AgentToolEffect = "idempotent_write"
	// AgentToolEffectNonIdempotentWrite marks every other Mutation: a repeat
	// may make a second effect.
	AgentToolEffectNonIdempotentWrite AgentToolEffect = "non_idempotent_write"
)

// AgentBranchOutcome classifies one optional branch for agents. It is the
// value of the manifest's agent.branches map.
type AgentBranchOutcome string

const (
	// AgentBranchOutcomeRejected means the call changed nothing: the provider
	// refused it, or the connector refused it before sending the request.
	AgentBranchOutcomeRejected AgentBranchOutcome = "rejected"
	// AgentBranchOutcomeUnknown means the operation's effect may have happened.
	// Only a Mutation branch may use it.
	AgentBranchOutcomeUnknown AgentBranchOutcome = "unknown"
)

// AgentOperationKind distinguishes Query tools from Mutation tools.
type AgentOperationKind string

const (
	// AgentOperationKindQuery is a read-only operation run through RunQuery.
	AgentOperationKindQuery AgentOperationKind = "query"
	// AgentOperationKindMutation is a provider write run through RunMutation.
	AgentOperationKindMutation AgentOperationKind = "mutation"
)

// AgentToolMetadata is the reviewed agent surface of one connector operation.
// connectorctl generates it from the operation's agent block in
// connector.yaml; NewQueryAgentTool and NewMutationAgentTool check it against
// the operation definition. Use it as a read-only value.
type AgentToolMetadata struct {
	// Operation identifies the connector operation the tool runs.
	Operation OperationRef
	// Kind says whether the operation is a Query or a Mutation.
	Kind AgentOperationKind
	// ToolName is the connector ID, an underscore, and the operation ID in
	// snake case, such as hacker-news-daily_list_story_ids. It names the
	// tool before a host adds any connection qualifier.
	ToolName string
	// Description is the text a model reads to choose the tool: the
	// manifest's agent.description, or else the operation description.
	Description string
	// IsApprovalRecommended reports agent.approval: recommended, which asks
	// the host to stop for a person before the call runs.
	IsApprovalRecommended bool
	// IsRetrySafe reports agent.retry: safe. Only a Mutation may set it.
	IsRetrySafe bool
	// BranchOutcomes is the manifest's agent.branches map. Keys are optional
	// branch IDs of the operation; a missing key fails closed, see RunAttempt.
	BranchOutcomes map[BranchID]AgentBranchOutcome
	// InputType is the Go type name of the operation input, as the generated
	// dex-connector.json records it.
	InputType string
	// InputSchema is the JSON Schema 2020-12 object of the operation input,
	// identical to the inputSchema in dex-connector.json. It is required.
	InputSchema json.RawMessage
	// Branches equals the operation definition's branches, so a host can
	// describe outcomes without an operation instance.
	Branches []BranchDefinition
	// StepDefaults equals the operation definition's Step defaults, so a host
	// can derive attempt timeouts and retry delays without an operation
	// instance.
	StepDefaults StepDefaults
}

// Effect returns the Dex effect class of the tool. A Query is read, a Mutation
// with IsRetrySafe is idempotent_write, and anything else, including an
// unrecognized Kind, is non_idempotent_write so that it fails closed.
func (metadata AgentToolMetadata) Effect() AgentToolEffect {
	switch {
	case metadata.Kind == AgentOperationKindQuery:
		return AgentToolEffectRead
	case metadata.Kind == AgentOperationKindMutation && metadata.IsRetrySafe:
		return AgentToolEffectIdempotentWrite
	default:
		return AgentToolEffectNonIdempotentWrite
	}
}

// AgentTool is one exposed connector operation bound to one connection, with
// its input and output types erased so an agent host can list and run tools
// of many connectors uniformly. It is sealed: construct it with
// NewQueryAgentTool or NewMutationAgentTool. An AgentTool is immutable and
// safe for concurrent use when its operation is.
type AgentTool interface {
	// Metadata returns a copy of the tool's validated metadata.
	Metadata() AgentToolMetadata
	// Connection returns the connection the tool's operation runs with.
	Connection() ConnectionRef
	// RunAttempt decodes arguments strictly into the operation input and runs
	// exactly one attempt through RunQuery or RunMutation, then classifies it.
	//
	// arguments must be one JSON object with no field the input type lacks;
	// empty or whitespace-only arguments mean {}. A decoding failure returns
	// an error wrapping ErrAgentToolArgumentsInvalid, and nothing is invoked.
	// Required fields and value ranges are not checked here: validate
	// arguments against InputSchema first.
	//
	// ctx must be a Dex Step context whose FlowID and StepExecutionID stay the
	// same for every attempt of one logical call, because the Call ID, and so
	// any provider idempotency key, derives from them. Attempt must count the
	// attempts of that call from one.
	//
	// The selected branch is classified in this order: uncertain is unknown;
	// on a Mutation, a defect that sdkgo selected after Invoke because the
	// returned attempt was invalid is unknown whatever BranchOutcomes says; a
	// required branch other than defect is succeeded; a listed branch takes
	// its listed outcome; an unlisted Mutation branch is unknown; an unlisted
	// Query branch is rejected. A connector Retry is AgentToolAttemptRetry.
	//
	// A nil error returns a classified attempt. A non-nil error means the
	// attempt ended without a branch: arguments did not decode, the operation
	// or IdempotencyKey panicked (ErrAgentToolPanicked), or the result value
	// did not encode as JSON. Only the returned HasInvokedOperation is
	// meaningful then; when it is true the provider may have been called.
	RunAttempt(ctx dex.Context, arguments json.RawMessage) (AgentToolAttempt, error)
	isAgentTool()
}

// AgentToolAttemptOutcome is the classified outcome of one RunAttempt.
type AgentToolAttemptOutcome string

const (
	// AgentToolAttemptSucceeded means the operation selected a required
	// (happy-path) branch.
	AgentToolAttemptSucceeded AgentToolAttemptOutcome = "succeeded"
	// AgentToolAttemptRejected means nothing took effect; the model may
	// correct its input and call again.
	AgentToolAttemptRejected AgentToolAttemptOutcome = "rejected"
	// AgentToolAttemptUnknown means a write may have taken effect; a host must
	// not let the model repeat the call as if it had failed.
	AgentToolAttemptUnknown AgentToolAttemptOutcome = "unknown"
	// AgentToolAttemptRetry means the connector returned Retry: the attempt is
	// safe to repeat with the same Call ID after RetryAfter.
	AgentToolAttemptRetry AgentToolAttemptOutcome = "retry"
)

// AgentToolAttempt is the classified result of one RunAttempt.
type AgentToolAttempt struct {
	// Outcome classifies the attempt for the agent host.
	Outcome AgentToolAttemptOutcome
	// Branch is the branch the attempt selected; it is empty for Retry.
	Branch BranchID
	// Value is the JSON encoding of the operation's result value; it is nil
	// for Retry.
	Value json.RawMessage
	// Failure is the connector's secret-safe failure, or nil on success. For
	// Retry it is the failure the connector retried.
	Failure *Failure
	// RetryAfter is the connector's requested delay before the next attempt
	// for Retry; zero lets the host choose from the operation's retry policy.
	RetryAfter time.Duration
	// HasInvokedOperation is false when the attempt ended before the
	// operation's Invoke ran, so the provider was not called.
	HasInvokedOperation bool
}

// ErrAgentToolArgumentsInvalid is wrapped by RunAttempt when arguments do not
// decode into the operation input. Nothing was invoked.
var ErrAgentToolArgumentsInvalid = errors.New("agent tool arguments do not decode into the operation input")

// ErrAgentToolPanicked is wrapped by RunAttempt when the operation panicked.
// The returned attempt's HasInvokedOperation says whether Invoke had started.
var ErrAgentToolPanicked = errors.New("agent tool operation panicked")

type queryAgentTool[IN, OUT any] struct {
	operation  Query[IN, OUT]
	connection ConnectionRef
	metadata   AgentToolMetadata
}

type mutationAgentTool[IN, OUT any] struct {
	operation  Mutation[IN, OUT]
	connection ConnectionRef
	metadata   AgentToolMetadata
}

// NewQueryAgentTool binds a Query operation to a connection as an AgentTool.
// It returns an error when operation is nil, the connection or the operation
// definition is invalid, or metadata does not match the definition: another
// OperationRef, a Kind other than query, IsRetrySafe set, a BranchOutcomes key
// that is not an optional branch or a value other than rejected, Branches or
// StepDefaults that differ from the definition, a blank ToolName or
// Description, a ToolName that does not start with the connector ID and an
// underscore, or an InputSchema that is not a JSON object. The tool keeps its
// own copy of metadata.
func NewQueryAgentTool[IN, OUT any](operation Query[IN, OUT], connection ConnectionRef, metadata AgentToolMetadata) (AgentTool, error) {
	if isNilValue(operation) {
		return nil, errors.New("agent tool Query operation is required")
	}
	definition := operation.Definition()
	if err := definition.Validate(); err != nil {
		return nil, fmt.Errorf("agent tool Query definition: %w", err)
	}
	if err := validateAgentToolMetadata(metadata, AgentOperationKindQuery, definition.Operation, definition.Branches, definition.StepDefaults, connection); err != nil {
		return nil, err
	}
	return queryAgentTool[IN, OUT]{operation: operation, connection: connection, metadata: cloneAgentToolMetadata(metadata)}, nil
}

// NewMutationAgentTool binds a Mutation operation to a connection as an
// AgentTool. It returns an error when operation is nil, the connection or the
// operation definition is invalid, or metadata does not match the definition:
// another OperationRef, a Kind other than mutation, a BranchOutcomes key that
// is not an optional branch, a value other than rejected or unknown, the
// uncertain branch mapped to rejected, Branches or StepDefaults that differ
// from the definition, a blank ToolName or Description, a ToolName that does
// not start with the connector ID and an underscore, or an InputSchema that is
// not a JSON object. The tool keeps its own copy of metadata.
func NewMutationAgentTool[IN, OUT any](operation Mutation[IN, OUT], connection ConnectionRef, metadata AgentToolMetadata) (AgentTool, error) {
	if isNilValue(operation) {
		return nil, errors.New("agent tool Mutation operation is required")
	}
	definition := operation.Definition()
	if err := definition.Validate(); err != nil {
		return nil, fmt.Errorf("agent tool Mutation definition: %w", err)
	}
	if err := validateAgentToolMetadata(metadata, AgentOperationKindMutation, definition.Operation, definition.Branches, definition.StepDefaults, connection); err != nil {
		return nil, err
	}
	return mutationAgentTool[IN, OUT]{operation: operation, connection: connection, metadata: cloneAgentToolMetadata(metadata)}, nil
}

// Metadata returns a copy of the tool's validated metadata.
func (tool queryAgentTool[IN, OUT]) Metadata() AgentToolMetadata {
	return cloneAgentToolMetadata(tool.metadata)
}

// Connection returns the connection the tool's operation runs with.
func (tool queryAgentTool[IN, OUT]) Connection() ConnectionRef { return tool.connection }

// RunAttempt runs and classifies one attempt; see AgentTool.RunAttempt.
func (tool queryAgentTool[IN, OUT]) RunAttempt(ctx dex.Context, arguments json.RawMessage) (attempt AgentToolAttempt, err error) {
	input, err := decodeAgentToolArguments[IN](tool.metadata.ToolName, arguments)
	if err != nil {
		return AgentToolAttempt{}, err
	}
	trace := &operationRunTrace{}
	defer recoverAgentToolPanic(tool.metadata.Operation, trace, &attempt, &err)
	result, runErr := runQueryWithTrace(ctx, tool.operation, tool.connection, input, trace)
	if runErr != nil {
		return agentToolRetryAttempt(tool.metadata.Operation, trace, runErr)
	}
	return classifyAgentToolResult(tool.metadata, trace, result.Branch, result.Value, result.Failure)
}

func (queryAgentTool[IN, OUT]) isAgentTool() {}

// Metadata returns a copy of the tool's validated metadata.
func (tool mutationAgentTool[IN, OUT]) Metadata() AgentToolMetadata {
	return cloneAgentToolMetadata(tool.metadata)
}

// Connection returns the connection the tool's operation runs with.
func (tool mutationAgentTool[IN, OUT]) Connection() ConnectionRef { return tool.connection }

// RunAttempt runs and classifies one attempt; see AgentTool.RunAttempt.
func (tool mutationAgentTool[IN, OUT]) RunAttempt(ctx dex.Context, arguments json.RawMessage) (attempt AgentToolAttempt, err error) {
	input, err := decodeAgentToolArguments[IN](tool.metadata.ToolName, arguments)
	if err != nil {
		return AgentToolAttempt{}, err
	}
	trace := &operationRunTrace{}
	defer recoverAgentToolPanic(tool.metadata.Operation, trace, &attempt, &err)
	result, runErr := runMutationWithTrace(ctx, tool.operation, tool.connection, input, trace)
	if runErr != nil {
		return agentToolRetryAttempt(tool.metadata.Operation, trace, runErr)
	}
	return classifyAgentToolResult(tool.metadata, trace, result.Branch, result.Value, result.Failure)
}

func (mutationAgentTool[IN, OUT]) isAgentTool() {}

// recoverAgentToolPanic reports only the panic value's type: its text may carry provider data.
func recoverAgentToolPanic(operation OperationRef, trace *operationRunTrace, attempt *AgentToolAttempt, err *error) {
	recovered := recover()
	if recovered == nil {
		return
	}
	*attempt = AgentToolAttempt{HasInvokedOperation: trace.hasInvokedOperation}
	*err = fmt.Errorf("%w: %s %s panicked with a %T value", ErrAgentToolPanicked, operation.ConnectorID, operation.OperationID, recovered)
}

func agentToolRetryAttempt(operation OperationRef, trace *operationRunTrace, runErr error) (AgentToolAttempt, error) {
	var retryErr *RetryError
	if !errors.As(runErr, &retryErr) {
		return AgentToolAttempt{HasInvokedOperation: trace.hasInvokedOperation}, fmt.Errorf("%s %s attempt: %w", operation.ConnectorID, operation.OperationID, runErr)
	}
	attempt := AgentToolAttempt{Outcome: AgentToolAttemptRetry, HasInvokedOperation: trace.hasInvokedOperation}
	failure := retryErr.Failure
	attempt.Failure = &failure
	var retryAfterErr *dex.RetryAfterError
	if errors.As(runErr, &retryAfterErr) {
		attempt.RetryAfter = retryAfterErr.After
	}
	return attempt, nil
}

func classifyAgentToolResult(metadata AgentToolMetadata, trace *operationRunTrace, branch BranchID, value any, failure *Failure) (AgentToolAttempt, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return AgentToolAttempt{HasInvokedOperation: trace.hasInvokedOperation}, fmt.Errorf("encode %s %s result value: %w", metadata.Operation.ConnectorID, metadata.Operation.OperationID, err)
	}
	return AgentToolAttempt{
		Outcome:             classifyAgentToolBranch(metadata, branch, trace.isDefectCreatedAfterInvoke),
		Branch:              branch,
		Value:               encoded,
		Failure:             failure,
		HasInvokedOperation: trace.hasInvokedOperation,
	}, nil
}

// classifyAgentToolBranch follows Dex's listing-first rule; sdkgo's own knowledge may only tighten it to unknown.
func classifyAgentToolBranch(metadata AgentToolMetadata, branch BranchID, isDefectCreatedAfterInvoke bool) AgentToolAttemptOutcome {
	isMutation := metadata.Kind == AgentOperationKindMutation
	if branch == UncertainBranchID {
		return AgentToolAttemptUnknown
	}
	if isMutation && branch == DefectBranchID && isDefectCreatedAfterInvoke {
		return AgentToolAttemptUnknown
	}
	if branch != DefectBranchID && isRequiredBranch(metadata.Branches, branch) {
		return AgentToolAttemptSucceeded
	}
	switch metadata.BranchOutcomes[branch] {
	case AgentBranchOutcomeRejected:
		return AgentToolAttemptRejected
	case AgentBranchOutcomeUnknown:
		return AgentToolAttemptUnknown
	}
	if isMutation {
		return AgentToolAttemptUnknown
	}
	return AgentToolAttemptRejected
}

func isRequiredBranch(branches []BranchDefinition, branch BranchID) bool {
	for _, candidate := range branches {
		if candidate.ID == branch {
			return !candidate.Optional
		}
	}
	return false
}

func decodeAgentToolArguments[IN any](toolName string, arguments json.RawMessage) (IN, error) {
	var input IN
	trimmed := bytes.TrimSpace(arguments)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}
	if trimmed[0] != '{' {
		return input, fmt.Errorf("%w: %s: arguments must be a JSON object", ErrAgentToolArgumentsInvalid, toolName)
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, fmt.Errorf("%w: %s: %w", ErrAgentToolArgumentsInvalid, toolName, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return input, fmt.Errorf("%w: %s: arguments contain data after the JSON object", ErrAgentToolArgumentsInvalid, toolName)
	}
	return input, nil
}

func validateAgentToolMetadata(metadata AgentToolMetadata, kind AgentOperationKind, operation OperationRef, branches []BranchDefinition, defaults StepDefaults, connection ConnectionRef) error {
	if err := connection.Validate(); err != nil {
		return fmt.Errorf("agent tool connection: %w", err)
	}
	if metadata.Operation != operation {
		return fmt.Errorf("agent tool metadata operation %s/%s does not match the operation definition %s/%s",
			metadata.Operation.ConnectorID, metadata.Operation.OperationID, operation.ConnectorID, operation.OperationID)
	}
	if metadata.Kind != kind {
		return fmt.Errorf("agent tool metadata kind %q does not match the operation kind %q", metadata.Kind, kind)
	}
	operationName, hasConnectorPrefix := strings.CutPrefix(metadata.ToolName, operation.ConnectorID+"_")
	if !hasConnectorPrefix || !agentToolOperationNamePattern.MatchString(operationName) {
		return fmt.Errorf("agent tool name %q must be %q, an underscore, and the snake case operation ID", metadata.ToolName, operation.ConnectorID)
	}
	if strings.TrimSpace(metadata.Description) == "" {
		return fmt.Errorf("agent tool %s description is required", metadata.ToolName)
	}
	if metadata.IsRetrySafe && kind != AgentOperationKindMutation {
		return fmt.Errorf("agent tool %s: only a Mutation may be retry safe", metadata.ToolName)
	}
	if !reflect.DeepEqual(metadata.Branches, branches) {
		return fmt.Errorf("agent tool %s branches do not match the operation definition", metadata.ToolName)
	}
	if !reflect.DeepEqual(metadata.StepDefaults, defaults) {
		return fmt.Errorf("agent tool %s Step defaults do not match the operation definition", metadata.ToolName)
	}
	for branch, outcome := range metadata.BranchOutcomes {
		if err := validateAgentBranchOutcome(kind, branches, branch, outcome); err != nil {
			return fmt.Errorf("agent tool %s branch %q: %w", metadata.ToolName, branch, err)
		}
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(metadata.InputSchema, &schema); err != nil || schema == nil {
		return fmt.Errorf("agent tool %s input schema must be a JSON object", metadata.ToolName)
	}
	return nil
}

func validateAgentBranchOutcome(kind AgentOperationKind, branches []BranchDefinition, branch BranchID, outcome AgentBranchOutcome) error {
	if !hasBranch(branches, branch) || isRequiredBranch(branches, branch) {
		return errors.New("is not an optional branch of the operation")
	}
	switch outcome {
	case AgentBranchOutcomeRejected:
		if branch == UncertainBranchID {
			return errors.New("uncertain cannot be rejected")
		}
		return nil
	case AgentBranchOutcomeUnknown:
		if kind != AgentOperationKindMutation {
			return errors.New("a Query branch may only be rejected")
		}
		return nil
	default:
		return fmt.Errorf("outcome %q must be rejected or unknown", outcome)
	}
}

// cloneAgentToolMetadata isolates a tool's validated metadata from its callers.
func cloneAgentToolMetadata(metadata AgentToolMetadata) AgentToolMetadata {
	metadata.BranchOutcomes = maps.Clone(metadata.BranchOutcomes)
	metadata.InputSchema = bytes.Clone(metadata.InputSchema)
	metadata.Branches = append([]BranchDefinition(nil), metadata.Branches...)
	metadata.StepDefaults.ExecuteRetry = cloneRetryPolicy(metadata.StepDefaults.ExecuteRetry)
	return metadata
}
