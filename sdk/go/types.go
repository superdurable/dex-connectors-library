// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package connector defines the provider-neutral contract used by Dex connectors.
package connector

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/dex"
)

var (
	connectorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	operationIDPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]+$`)
	callIDNamespace    = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://superdurable.dev/dex-connectors/call-id/v1"))
)

// CallID is the stable identity of one provider call across retries.
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
	Invoke(Call, IN) (QueryResult[OUT], error)
}

type Mutation[IN, OUT any] interface {
	Definition() MutationDefinition
	Invoke(Call, IN) (MutationResult[OUT], error)
}

// Call carries Dex execution identity and provider-safe call metadata.
// Applications receive Call from RunQuery or RunMutation and do not construct it.
type Call struct {
	Context    dex.Context   `json:"-"`
	ID         CallID        `json:"id"`
	Connection ConnectionRef `json:"connection"`
	Operation  OperationRef  `json:"operation"`
}

type QueryResult[T any] struct {
	Value   T       `json:"value"`
	Receipt Receipt `json:"receipt"`
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
	Provider          string            `json:"provider"`
	ProviderObjectID  string            `json:"providerObjectId,omitempty"`
	ProviderRequestID string            `json:"providerRequestId,omitempty"`
	ObservedAt        time.Time         `json:"observedAt"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

func RunQuery[IN, OUT any](ctx dex.Context, operation Query[IN, OUT], connection ConnectionRef, input IN) (QueryResult[OUT], error) {
	if operation == nil {
		return QueryResult[OUT]{}, fmt.Errorf("connector query is required")
	}
	call, err := newCall(ctx, operation.Definition().Operation, connection)
	if err != nil {
		return QueryResult[OUT]{}, err
	}
	result, err := operation.Invoke(call, input)
	if err != nil {
		return QueryResult[OUT]{}, err
	}
	result.Receipt, err = completeReceipt(result.Receipt, call)
	if err != nil {
		return QueryResult[OUT]{}, err
	}
	return result, nil
}

func RunMutation[IN, OUT any](ctx dex.Context, operation Mutation[IN, OUT], connection ConnectionRef, input IN) (MutationResult[OUT], error) {
	if operation == nil {
		return MutationResult[OUT]{}, fmt.Errorf("connector mutation is required")
	}
	call, err := newCall(ctx, operation.Definition().Operation, connection)
	if err != nil {
		return MutationResult[OUT]{}, err
	}
	result, err := operation.Invoke(call, input)
	if err != nil {
		return normalizeMutationError[OUT](call, err)
	}
	if result.Outcome != MutationSucceeded && result.Outcome != MutationFailed && result.Outcome != MutationUnknown {
		return MutationResult[OUT]{}, NewError(ErrorLocalDefect, call.Operation.ConnectorID, call.Operation.OperationID, "mutation returned an invalid outcome", nil)
	}
	if result.Outcome != MutationSucceeded && result.Failure == nil {
		return MutationResult[OUT]{}, NewError(ErrorLocalDefect, call.Operation.ConnectorID, call.Operation.OperationID, "non-successful mutation returned no failure", nil)
	}
	result.Receipt, err = completeReceipt(result.Receipt, call)
	if err != nil {
		return MutationResult[OUT]{}, err
	}
	return result, nil
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
		return Receipt{}, NewError(ErrorLocalDefect, call.Operation.ConnectorID, call.Operation.OperationID, "operation returned a receipt for another call", nil)
	}
	if receipt.CallID == "" {
		receipt.CallID = call.ID
	}
	if receipt.Provider == "" {
		receipt.Provider = call.Operation.ConnectorID
	}
	if receipt.ObservedAt.IsZero() {
		receipt.ObservedAt = time.Now().UTC()
	}
	return receipt, nil
}
