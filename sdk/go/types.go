// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package connector defines the provider-neutral contract used by Dex connectors.
package connector

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
	Provider string `json:"provider" yaml:"provider"`
	Name     string `json:"name" yaml:"name"`
}

func (ref ConnectionRef) Validate() error {
	if strings.TrimSpace(ref.Provider) == "" || strings.TrimSpace(ref.Name) == "" {
		return fmt.Errorf("connection provider and name are required")
	}
	return nil
}

// OperationRef is the stable manifest identity of one connector operation.
type OperationRef struct {
	ConnectorID string `json:"connectorId" yaml:"connectorId"`
	OperationID string `json:"operationId" yaml:"operationId"`
}

func (ref OperationRef) Validate() error {
	if !connectorIDPattern.MatchString(ref.ConnectorID) {
		return fmt.Errorf("connector ID must be DNS-like and 2-63 characters")
	}
	if !operationIDPattern.MatchString(ref.OperationID) {
		return fmt.Errorf("operation ID must be lower camel case")
	}
	return nil
}

type QueryDefinition struct {
	Operation OperationRef `json:"operation" yaml:"operation"`
}

type MutationDefinition struct {
	Operation OperationRef `json:"operation" yaml:"operation"`
}

type Query[IN, OUT any] interface {
	Definition() QueryDefinition
	Invoke(Call, IN) QueryAttempt[OUT]
}

type Mutation[IN, OUT any] interface {
	Definition() MutationDefinition
	IdempotencyKey(CallID, IN) IdempotencyKey
	Invoke(Call, IN) MutationAttempt[OUT]
}

// Call carries Dex identity and provider-safe metadata for one invocation.
type Call struct {
	Context        dex.Context    `json:"-"`
	ID             CallID         `json:"id"`
	IdempotencyKey IdempotencyKey `json:"idempotencyKey,omitempty"`
	Connection     ConnectionRef  `json:"connection"`
	Operation      OperationRef   `json:"operation"`
	progress       *progressReporter
	text           *dex.BufferedTextStream
}

type QueryOutcome string

const (
	QuerySucceeded QueryOutcome = "SUCCEEDED"
	QueryFailed    QueryOutcome = "FAILED"
)

type QueryResult[T any] struct {
	Outcome QueryOutcome `json:"outcome"`
	Value   T            `json:"value"`
	Receipt Receipt      `json:"receipt"`
	Failure *Failure     `json:"failure,omitempty"`
}

type MutationOutcome string

const (
	MutationSucceeded MutationOutcome = "SUCCEEDED"
	MutationFailed    MutationOutcome = "FAILED"
	MutationUnknown   MutationOutcome = "UNKNOWN"
)

type MutationResult[T any] struct {
	Outcome MutationOutcome `json:"outcome"`
	Value   T               `json:"value"`
	Receipt Receipt         `json:"receipt"`
	Failure *Failure        `json:"failure,omitempty"`
}

// Receipt contains only safe provider correlation data.
type Receipt struct {
	CallID            CallID            `json:"callId"`
	IdempotencyKey    IdempotencyKey    `json:"idempotencyKey,omitempty"`
	Provider          string            `json:"provider"`
	ProviderObjectID  string            `json:"providerObjectId,omitempty"`
	ProviderRequestID string            `json:"providerRequestId,omitempty"`
	ObservedAt        time.Time         `json:"observedAt"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// Progress contains provider progress before Dex identity is added.
type Progress struct {
	Phase            string
	Message          string
	Percent          *float64
	ProviderSequence *int64
}

// ProgressUpdate is a structured, best-effort Dex Stream message.
type ProgressUpdate struct {
	CallID           CallID   `json:"callId"`
	Attempt          int32    `json:"attempt"`
	Sequence         uint64   `json:"sequence"`
	Phase            string   `json:"phase"`
	Message          string   `json:"message,omitempty"`
	Percent          *float64 `json:"percent,omitempty"`
	ProviderSequence *int64   `json:"providerSequence,omitempty"`
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

func (call Call) HasProgressStream() bool { return call.progress != nil }

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

func RunQuery[IN, OUT any](ctx dex.Context, operation Query[IN, OUT], connection ConnectionRef, input IN, options ...RunOption) (QueryResult[OUT], error) {
	if operation == nil {
		return failedQuery[OUT](OperationRef{}, "connector query is required"), nil
	}
	definition := operation.Definition().Operation
	call, err := newCall(ctx, definition, connection)
	if err != nil {
		return failedQuery[OUT](definition, err.Error()), nil
	}
	if err := configureCall(&call, options); err != nil {
		return failedQueryForCall[OUT](call, err.Error()), nil
	}
	return queryResult(call, operation.Invoke(call, input))
}

func RunMutation[IN, OUT any](ctx dex.Context, operation Mutation[IN, OUT], connection ConnectionRef, input IN, options ...RunOption) (MutationResult[OUT], error) {
	if operation == nil {
		return failedMutation[OUT](OperationRef{}, "connector mutation is required"), nil
	}
	definition := operation.Definition().Operation
	call, err := newCall(ctx, definition, connection)
	if err != nil {
		return failedMutation[OUT](definition, err.Error()), nil
	}
	call.IdempotencyKey = operation.IdempotencyKey(call.ID, input)
	if call.IdempotencyKey == "" {
		call.IdempotencyKey = IdempotencyKey(call.ID)
	}
	if strings.TrimSpace(string(call.IdempotencyKey)) == "" {
		call.IdempotencyKey = ""
		return failedMutationForCall[OUT](call, "mutation returned an invalid idempotency key"), nil
	}
	if err := configureCall(&call, options); err != nil {
		return failedMutationForCall[OUT](call, err.Error()), nil
	}
	return mutationResult(call, operation.Invoke(call, input))
}

func queryResult[T any](call Call, attempt QueryAttempt[T]) (QueryResult[T], error) {
	switch attempt.kind {
	case queryAttemptSucceeded:
		receipt, err := completeReceipt(attempt.receipt, call)
		if err != nil {
			return failedQueryForCall[T](call, err.Error()), nil
		}
		return QueryResult[T]{Outcome: QuerySucceeded, Value: attempt.value, Receipt: receipt}, nil
	case queryAttemptFailed:
		if err := attempt.failure.validate(); err != nil {
			return failedQueryForCall[T](call, "query returned an invalid failure: "+err.Error()), nil
		}
		receipt, err := completeReceipt(attempt.receipt, call)
		if err != nil {
			return failedQueryForCall[T](call, err.Error()), nil
		}
		return QueryResult[T]{Outcome: QueryFailed, Value: attempt.value, Receipt: receipt, Failure: &attempt.failure}, nil
	case queryAttemptRetry:
		if err := attempt.failure.validate(); err != nil || attempt.retryAfter < 0 {
			return failedQueryForCall[T](call, "query returned an invalid retry attempt"), nil
		}
		return QueryResult[T]{}, retryError(attempt.failure, attempt.retryAfter)
	default:
		return failedQueryForCall[T](call, "query returned an invalid attempt"), nil
	}
}

func mutationResult[T any](call Call, attempt MutationAttempt[T]) (MutationResult[T], error) {
	switch attempt.kind {
	case mutationAttemptSucceeded:
		receipt, err := completeReceipt(attempt.receipt, call)
		if err != nil {
			return unknownMutationForCall[T](call, err.Error()), nil
		}
		return MutationResult[T]{Outcome: MutationSucceeded, Value: attempt.value, Receipt: receipt}, nil
	case mutationAttemptFailed, mutationAttemptUnknown:
		if err := attempt.failure.validate(); err != nil {
			return unknownMutationForCall[T](call, "mutation returned an invalid failure: "+err.Error()), nil
		}
		receipt, err := completeReceipt(attempt.receipt, call)
		if err != nil {
			return unknownMutationForCall[T](call, err.Error()), nil
		}
		outcome := MutationFailed
		if attempt.kind == mutationAttemptUnknown {
			outcome = MutationUnknown
		}
		return MutationResult[T]{Outcome: outcome, Value: attempt.value, Receipt: receipt, Failure: &attempt.failure}, nil
	case mutationAttemptRetry:
		if err := attempt.failure.validate(); err != nil || attempt.retryAfter < 0 {
			return unknownMutationForCall[T](call, "mutation returned an invalid retry attempt"), nil
		}
		return MutationResult[T]{}, retryError(attempt.failure, attempt.retryAfter)
	default:
		return unknownMutationForCall[T](call, "mutation returned an invalid attempt"), nil
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

func failedQuery[T any](operation OperationRef, message string) QueryResult[T] {
	failure := localFailure(operation, message)
	return QueryResult[T]{Outcome: QueryFailed, Failure: &failure}
}

func failedQueryForCall[T any](call Call, message string) QueryResult[T] {
	result := failedQuery[T](call.Operation, message)
	result.Receipt, _ = completeReceipt(Receipt{}, call)
	return result
}

func failedMutation[T any](operation OperationRef, message string) MutationResult[T] {
	failure := localFailure(operation, message)
	return MutationResult[T]{Outcome: MutationFailed, Failure: &failure}
}

func failedMutationForCall[T any](call Call, message string) MutationResult[T] {
	result := failedMutation[T](call.Operation, message)
	result.Receipt, _ = completeReceipt(Receipt{}, call)
	return result
}

func unknownMutationForCall[T any](call Call, message string) MutationResult[T] {
	failure := localFailure(call.Operation, message)
	receipt, _ := completeReceipt(Receipt{}, call)
	return MutationResult[T]{Outcome: MutationUnknown, Receipt: receipt, Failure: &failure}
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
