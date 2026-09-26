// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sdkgo defines the provider-neutral contract used by Dex connectors.
package sdkgo

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/dex"
)

var (
	connectorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	operationIDPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]+$`)
	callIDNamespace    = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://superdurable.dev/dex-connectors/call-id/v1"))
)

// CallID is the stable identity of one provider call across Step retries.
type CallID string

// Validate returns an error when the value violates its public contract.
func (id CallID) Validate() error {
	if id == "" {
		return fmt.Errorf("call ID is required")
	}
	if _, err := uuid.Parse(string(id)); err != nil {
		return fmt.Errorf("call ID must be a UUID: %w", err)
	}
	return nil
}

// IdempotencyKey is the provider-specific form of a stable CallID.
type IdempotencyKey string

// ConnectionRef is a logical credential reference safe to persist in a Flow.
type ConnectionRef struct {
	// Provider names the external provider without exposing credentials.
	Provider string `json:"provider" yaml:"provider"`
	// Name is the configured connection name within Provider.
	Name string `json:"name" yaml:"name"`
}

// Validate returns an error when the value violates its public contract.
func (ref ConnectionRef) Validate() error {
	if strings.TrimSpace(ref.Provider) == "" || strings.TrimSpace(ref.Name) == "" {
		return fmt.Errorf("connection provider and name are required")
	}
	return nil
}

// OperationRef is the stable manifest identity of one connector operation.
type OperationRef struct {
	// ConnectorID identifies the connector manifest.
	ConnectorID string `json:"connectorId" yaml:"connectorId"`
	// OperationID identifies the operation within its connector manifest.
	OperationID string `json:"operationId" yaml:"operationId"`
}

// Validate returns an error when the value violates its public contract.
func (ref OperationRef) Validate() error {
	if !connectorIDPattern.MatchString(ref.ConnectorID) {
		return fmt.Errorf("connector ID must be DNS-like and 2-63 characters")
	}
	if !operationIDPattern.MatchString(ref.OperationID) {
		return fmt.Errorf("operation ID must be lower camel case")
	}
	return nil
}

// BranchID is one stable, operation-defined process route.
type BranchID string

const (
	// DefectBranchID is the standard route for local Connector defects.
	DefectBranchID BranchID = "defect"
	// UncertainBranchID is the optional route for dispatched mutations with unknown outcomes.
	UncertainBranchID BranchID = "uncertain"
)

// BranchDefinition is one stable route an operation can select.
type BranchDefinition struct {
	// ID is the stable branch name returned in a Query or Mutation result.
	ID BranchID `json:"id" yaml:"id"`
	// Description explains the route for connector authors and generated docs.
	Description string `json:"description" yaml:"description"`
	// Optional means an application may omit the branch target.
	// An omitted optional branch fails the Flow when the operation selects it.
	// Leave this false for a happy-path branch the application must connect.
	Optional bool `json:"optional,omitempty" yaml:"optional,omitempty"`
}

// StepDefaults are the execute-only Dex options owned by an operation definition.
type StepDefaults struct {
	// ExecuteMethodTimeout sets the maximum Execute duration; zero uses the Dex default.
	ExecuteMethodTimeout time.Duration `json:"executeMethodTimeout" yaml:"executeMethodTimeout"`
	// HeartbeatTimeout sets the heartbeat timeout; zero disables heartbeats.
	HeartbeatTimeout time.Duration `json:"heartbeatTimeout,omitempty" yaml:"heartbeatTimeout,omitempty"`
	// ExecuteRetry sets the Dex Execute retry policy; nil uses the Dex default.
	ExecuteRetry *dex.RetryPolicy `json:"executeRetry,omitempty" yaml:"executeRetry,omitempty"`
	// ExecuteDurability selects the Dex Execute durability mode.
	ExecuteDurability dex.StepDurability `json:"executeDurability" yaml:"executeDurability"`
}

// QueryDefinition declares a query's identity, result branches, and Dex Step defaults.
type QueryDefinition struct {
	// Operation identifies the connector operation.
	Operation OperationRef `json:"operation" yaml:"operation"`
	// Branches declares the operation's stable terminal routes.
	Branches []BranchDefinition `json:"branches" yaml:"branches"`
	// StepDefaults provides the operation-owned Dex Execute defaults.
	StepDefaults StepDefaults `json:"stepDefaults" yaml:"stepDefaults"`
}

// MutationDefinition declares a mutation's identity, result branches, and Dex Step defaults.
type MutationDefinition struct {
	// Operation identifies the connector operation.
	Operation OperationRef `json:"operation" yaml:"operation"`
	// Branches declares the operation's stable terminal routes.
	Branches []BranchDefinition `json:"branches" yaml:"branches"`
	// StepDefaults provides the operation-owned Dex Execute defaults.
	StepDefaults StepDefaults `json:"stepDefaults" yaml:"stepDefaults"`
}

// Query defines a read-only provider operation invoked by RunQuery.
type Query[IN, OUT any] interface {
	// Definition returns the immutable connector operation definition.
	Definition() QueryDefinition
	// Invoke executes one provider call and classifies its attempt.
	Invoke(Call, IN) QueryAttempt[OUT]
}

// Mutation defines an idempotent provider operation invoked by RunMutation.
type Mutation[IN, OUT any] interface {
	// Definition returns the immutable connector operation definition.
	Definition() MutationDefinition
	// IdempotencyKey derives a provider-safe key from the stable call identity.
	IdempotencyKey(CallID, IN) IdempotencyKey
	// Invoke executes one provider call and classifies its attempt.
	Invoke(Call, IN) MutationAttempt[OUT]
}

// Call carries Dex identity and provider-safe metadata for one invocation.
type Call struct {
	// Context is the active Dex Step context and is never serialized.
	Context dex.Context `json:"-"`
	// ID is the stable connector call identity derived from Dex execution identity.
	ID CallID `json:"id"`
	// IdempotencyKey is the provider-safe deduplication key for the call.
	IdempotencyKey IdempotencyKey `json:"idempotencyKey,omitempty"`
	// Connection selects the credential reference for this invocation.
	Connection ConnectionRef `json:"connection"`
	// Operation identifies the connector operation.
	Operation OperationRef `json:"operation"`
	progress  *progressReporter
	text      *dex.BufferedTextStream
}

// QueryResult is the validated terminal result returned by RunQuery.
type QueryResult[T any] struct {
	// Branch is the stable route selected by the operation.
	Branch BranchID `json:"branch"`
	// Value is the typed operation result.
	Value T `json:"value"`
	// Receipt contains safe provider correlation metadata.
	Receipt Receipt `json:"receipt"`
	// Failure describes the terminal failure, or is nil on success.
	Failure *Failure `json:"failure,omitempty"`
}

// MutationResult is the validated terminal result returned by RunMutation.
type MutationResult[T any] struct {
	// Branch is the stable route selected by the operation.
	Branch BranchID `json:"branch"`
	// Value is the typed operation result.
	Value T `json:"value"`
	// Receipt contains safe provider correlation metadata.
	Receipt Receipt `json:"receipt"`
	// Failure describes the terminal failure, or is nil on success.
	Failure *Failure `json:"failure,omitempty"`
}

// Receipt contains only safe provider correlation data.
type Receipt struct {
	// CallID identifies the stable provider call across Dex retries.
	CallID CallID `json:"callId"`
	// IdempotencyKey is the provider-safe deduplication key for the call.
	IdempotencyKey IdempotencyKey `json:"idempotencyKey,omitempty"`
	// Provider names the external provider without exposing credentials.
	Provider string `json:"provider"`
	// ProviderObjectID is the provider object's safe correlation identifier.
	ProviderObjectID string `json:"providerObjectId,omitempty"`
	// ProviderRequestID is the provider request's safe correlation identifier.
	ProviderRequestID string `json:"providerRequestId,omitempty"`
	// ObservedAt records when the provider result was observed.
	ObservedAt time.Time `json:"observedAt"`
	// Metadata contains additional safe provider correlation values.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Progress contains provider progress before Dex identity is added.
type Progress struct {
	// Phase is a stable provider-specific progress phase.
	Phase string
	// Message is optional provider-safe progress text.
	Message string
	// Percent is optional progress in the inclusive range 0 through 1.
	Percent *float64
	// ProviderSequence is the provider's optional monotonic progress sequence.
	ProviderSequence *int64
}

// ProgressUpdate is a structured, best-effort Dex Stream message.
type ProgressUpdate struct {
	// CallID identifies the stable provider call across Dex retries.
	CallID CallID `json:"callId"`
	// Attempt is the one-based Dex Execute attempt number.
	Attempt int32 `json:"attempt"`
	// Sequence is the connector-assigned monotonic progress sequence.
	Sequence uint64 `json:"sequence"`
	// Phase is the stable provider-specific progress phase.
	Phase string `json:"phase"`
	// Message is optional provider-safe progress text.
	Message string `json:"message,omitempty"`
	// Percent is optional progress in the inclusive range 0 through 1.
	Percent *float64 `json:"percent,omitempty"`
	// ProviderSequence is the provider's optional monotonic progress sequence.
	ProviderSequence *int64 `json:"providerSequence,omitempty"`
}

type progressReporter struct {
	mu       sync.Mutex
	stream   dex.Stream[ProgressUpdate]
	sequence uint64
}

// ReportProgress appends one structured update when a progress Stream is configured.
func (call Call) ReportProgress(progress Progress) error {
	if call.progress == nil {
		return nil
	}
	if progress.Percent != nil && (*progress.Percent < 0 || *progress.Percent > 1) {
		return fmt.Errorf("connector progress percent must be between 0 and 1")
	}
	call.progress.mu.Lock()
	defer call.progress.mu.Unlock()
	call.progress.sequence++
	return call.progress.stream.Write(call.Context, ProgressUpdate{
		CallID: call.ID, Attempt: call.Context.Attempt(), Sequence: call.progress.sequence,
		Phase: progress.Phase, Message: progress.Message, Percent: progress.Percent,
		ProviderSequence: progress.ProviderSequence,
	})
}

// WriteText appends text in byte order when a text Stream is configured.
func (call Call) WriteText(value string) error {
	if call.text == nil {
		return nil
	}
	return call.text.Write(value)
}

// HasProgressStream reports whether structured progress output is configured.
func (call Call) HasProgressStream() bool { return call.progress != nil }

// HasTextStream reports whether buffered text output is configured.
func (call Call) HasTextStream() bool { return call.text != nil }

type runConfig struct {
	progressStream *dex.Stream[ProgressUpdate]
	textStream     *dex.Stream[string]
	textOptions    []dex.BufferedTextStreamOption
}

// RunOption configures optional best-effort progress for one operation invocation.
type RunOption interface {
	apply(*runConfig) error
}

type progressStreamOption struct{ stream dex.Stream[ProgressUpdate] }

func (option progressStreamOption) apply(config *runConfig) error {
	if option.stream.StreamName() == "" {
		return fmt.Errorf("progress Stream is not defined")
	}
	config.progressStream = &option.stream
	return nil
}

// WithProgressStream emits structured progress to an application-owned Dex Stream.
func WithProgressStream(stream dex.Stream[ProgressUpdate]) RunOption {
	return progressStreamOption{stream: stream}
}

type textStreamOption struct {
	stream  dex.Stream[string]
	options []dex.BufferedTextStreamOption
}

func (option textStreamOption) apply(config *runConfig) error {
	if option.stream.StreamName() == "" {
		return fmt.Errorf("text Stream is not defined")
	}
	config.textStream = &option.stream
	config.textOptions = option.options
	return nil
}

// WithTextStream emits ordered text through Dex BufferedTextStream.
func WithTextStream(stream dex.Stream[string], options ...dex.BufferedTextStreamOption) RunOption {
	return textStreamOption{stream: stream, options: append([]dex.BufferedTextStreamOption(nil), options...)}
}

// RunQuery invokes a query inside a Dex Step and validates its terminal attempt.
func RunQuery[IN, OUT any](ctx dex.Context, operation Query[IN, OUT], connection ConnectionRef, input IN, options ...RunOption) (QueryResult[OUT], error) {
	if isNilValue(operation) {
		return failedQuery[OUT](QueryDefinition{}, "connector query is required"), nil
	}
	definition := operation.Definition()
	if err := definition.Validate(); err != nil {
		return failedQuery[OUT](definition, "invalid query definition: "+err.Error()), nil
	}
	call, err := newCall(ctx, definition.Operation, connection)
	if err != nil {
		return failedQuery[OUT](definition, err.Error()), nil
	}
	if err := configureCall(&call, options); err != nil {
		return failedQueryForCall[OUT](definition, call, err.Error()), nil
	}
	return queryResult(call, definition, operation.Invoke(call, input))
}

// RunMutation invokes a mutation inside a Dex Step using a stable idempotency key.
func RunMutation[IN, OUT any](ctx dex.Context, operation Mutation[IN, OUT], connection ConnectionRef, input IN, options ...RunOption) (MutationResult[OUT], error) {
	if isNilValue(operation) {
		return failedMutation[OUT](MutationDefinition{}, "connector mutation is required"), nil
	}
	definition := operation.Definition()
	if err := definition.Validate(); err != nil {
		return failedMutation[OUT](definition, "invalid mutation definition: "+err.Error()), nil
	}
	call, err := newCall(ctx, definition.Operation, connection)
	if err != nil {
		return failedMutation[OUT](definition, err.Error()), nil
	}
	call.IdempotencyKey = operation.IdempotencyKey(call.ID, input)
	if call.IdempotencyKey == "" {
		call.IdempotencyKey = IdempotencyKey(call.ID)
	}
	if strings.TrimSpace(string(call.IdempotencyKey)) == "" {
		call.IdempotencyKey = ""
		return failedMutationForCall[OUT](definition, call, "mutation returned an invalid idempotency key"), nil
	}
	if err := configureCall(&call, options); err != nil {
		return failedMutationForCall[OUT](definition, call, err.Error()), nil
	}
	return mutationResult(call, definition, operation.Invoke(call, input))
}

func queryResult[T any](call Call, definition QueryDefinition, attempt QueryAttempt[T]) (QueryResult[T], error) {
	switch attempt.kind {
	case queryAttemptBranch:
		if !definition.hasBranch(attempt.branch) {
			return failedQueryForCall[T](definition, call, "query returned an unknown branch"), nil
		}
		if attempt.hasFailure {
			if err := attempt.failure.validate(); err != nil {
				return failedQueryForCall[T](definition, call, "query returned an invalid failure: "+err.Error()), nil
			}
		}
		receipt, err := completeReceipt(attempt.receipt, call)
		if err != nil {
			return failedQueryForCall[T](definition, call, err.Error()), nil
		}
		result := QueryResult[T]{Branch: attempt.branch, Value: attempt.value, Receipt: receipt}
		if attempt.hasFailure {
			result.Failure = &attempt.failure
		}
		return result, nil
	case queryAttemptRetry:
		if err := attempt.failure.validate(); err != nil || attempt.retryAfter < 0 {
			return failedQueryForCall[T](definition, call, "query returned an invalid retry attempt"), nil
		}
		return QueryResult[T]{}, retryError(attempt.failure, attempt.retryAfter)
	default:
		return failedQueryForCall[T](definition, call, "query returned an invalid attempt"), nil
	}
}

func mutationResult[T any](call Call, definition MutationDefinition, attempt MutationAttempt[T]) (MutationResult[T], error) {
	switch attempt.kind {
	case mutationAttemptBranch:
		if !definition.hasBranch(attempt.branch) {
			return failedMutationForCall[T](definition, call, "mutation returned an unknown branch"), nil
		}
		if attempt.hasFailure {
			if err := attempt.failure.validate(); err != nil {
				return failedMutationForCall[T](definition, call, "mutation returned an invalid failure: "+err.Error()), nil
			}
		}
		receipt, err := completeReceipt(attempt.receipt, call)
		if err != nil {
			return failedMutationForCall[T](definition, call, err.Error()), nil
		}
		result := MutationResult[T]{Branch: attempt.branch, Value: attempt.value, Receipt: receipt}
		if attempt.hasFailure {
			result.Failure = &attempt.failure
		}
		return result, nil
	case mutationAttemptUncertain:
		if !definition.hasBranch(UncertainBranchID) {
			return failedMutationForCall[T](definition, call, "mutation returned uncertainty without declaring the uncertain branch"), nil
		}
		if err := attempt.failure.validate(); err != nil {
			return failedMutationForCall[T](definition, call, "mutation returned an invalid uncertainty failure: "+err.Error()), nil
		}
		receipt, err := completeReceipt(attempt.receipt, call)
		if err != nil {
			return failedMutationForCall[T](definition, call, err.Error()), nil
		}
		return MutationResult[T]{Branch: UncertainBranchID, Value: attempt.value, Receipt: receipt, Failure: &attempt.failure}, nil
	case mutationAttemptRetry:
		if err := attempt.failure.validate(); err != nil || attempt.retryAfter < 0 {
			return failedMutationForCall[T](definition, call, "mutation returned an invalid retry attempt"), nil
		}
		return MutationResult[T]{}, retryError(attempt.failure, attempt.retryAfter)
	default:
		return failedMutationForCall[T](definition, call, "mutation returned an invalid attempt"), nil
	}
}

func retryError(failure Failure, delay time.Duration) error {
	err := &RetryError{Failure: failure}
	if delay > 0 {
		return dex.RetryAfter(delay, err)
	}
	return err
}

func newCall(ctx dex.Context, operation OperationRef, connection ConnectionRef) (Call, error) {
	if ctx == nil {
		return Call{}, fmt.Errorf("Dex context is required")
	}
	if ctx.FlowID() == "" {
		return Call{}, fmt.Errorf("Dex Flow ID is required")
	}
	if ctx.StepExecutionID() == "" {
		return Call{}, fmt.Errorf("connector operations require a Dex Step execution; RPC invocation is not allowed")
	}
	if err := operation.Validate(); err != nil {
		return Call{}, fmt.Errorf("invalid connector operation: %w", err)
	}
	if err := connection.Validate(); err != nil {
		return Call{}, fmt.Errorf("invalid connector connection: %w", err)
	}
	return Call{
		Context: ctx, ID: deriveCallID(ctx.FlowID(), ctx.StepExecutionID(), operation),
		Connection: connection, Operation: operation,
	}, nil
}

func configureCall(call *Call, options []RunOption) error {
	config := runConfig{}
	for _, option := range options {
		if option == nil {
			return fmt.Errorf("connector RunOption is nil")
		}
		if err := option.apply(&config); err != nil {
			return err
		}
	}
	if config.progressStream != nil {
		call.progress = &progressReporter{stream: *config.progressStream}
	}
	if config.textStream != nil {
		writer, err := dex.NewBufferedTextStream(call.Context, *config.textStream, config.textOptions...)
		if err != nil {
			return fmt.Errorf("configure text progress: %w", err)
		}
		call.text = writer
	}
	return nil
}

func deriveCallID(flowID, stepExecutionID string, operation OperationRef) CallID {
	fields := []string{flowID, stepExecutionID, operation.ConnectorID, operation.OperationID}
	payload := make([]byte, 0, len(flowID)+len(stepExecutionID)+len(operation.ConnectorID)+len(operation.OperationID)+16)
	var length [4]byte
	for _, field := range fields {
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		payload = append(payload, length[:]...)
		payload = append(payload, field...)
	}
	return CallID(uuid.NewSHA1(callIDNamespace, payload).String())
}

func completeReceipt(receipt Receipt, call Call) (Receipt, error) {
	if receipt.CallID != "" && receipt.CallID != call.ID {
		return Receipt{}, fmt.Errorf("operation returned a receipt for another call")
	}
	if receipt.IdempotencyKey != "" && receipt.IdempotencyKey != call.IdempotencyKey {
		return Receipt{}, fmt.Errorf("operation returned a receipt with another idempotency key")
	}
	if receipt.CallID == "" {
		receipt.CallID = call.ID
	}
	if call.IdempotencyKey != "" {
		receipt.IdempotencyKey = call.IdempotencyKey
	}
	if receipt.Provider == "" {
		receipt.Provider = call.Operation.ConnectorID
	}
	if receipt.ObservedAt.IsZero() {
		receipt.ObservedAt = time.Now().UTC()
	}
	return receipt, nil
}

func mustCompleteEmptyReceipt(call Call) Receipt {
	receipt, err := completeReceipt(Receipt{}, call)
	if err != nil {
		panic(fmt.Sprintf("complete empty connector receipt: %v", err))
	}
	return receipt
}

func failedQuery[T any](definition QueryDefinition, message string) QueryResult[T] {
	failure := localFailure(definition.Operation, message)
	return QueryResult[T]{Branch: DefectBranchID, Failure: &failure}
}

func failedQueryForCall[T any](definition QueryDefinition, call Call, message string) QueryResult[T] {
	result := failedQuery[T](definition, message)
	result.Receipt = mustCompleteEmptyReceipt(call)
	return result
}

func failedMutation[T any](definition MutationDefinition, message string) MutationResult[T] {
	failure := localFailure(definition.Operation, message)
	return MutationResult[T]{Branch: DefectBranchID, Failure: &failure}
}

func failedMutationForCall[T any](definition MutationDefinition, call Call, message string) MutationResult[T] {
	result := failedMutation[T](definition, message)
	result.Receipt = mustCompleteEmptyReceipt(call)
	return result
}

func localFailure(operation OperationRef, message string) Failure {
	provider := operation.ConnectorID
	if provider == "" {
		provider = "connector"
	}
	operationID := operation.OperationID
	if operationID == "" {
		operationID = "invoke"
	}
	return Failure{Kind: FailureLocalDefect, Provider: provider, Operation: operationID, Message: message}
}
