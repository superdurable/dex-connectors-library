// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connectormock

import (
	"sync"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Call is one call a mock answered, as returned by Calls.
type Call[IN any] struct {
	// FlowID is the Flow whose Step made the call.
	FlowID string
	// CallID is the SDK's stable call identity for the Step execution.
	CallID sdkgo.CallID
	// IdempotencyKey is the Mutation's key, or empty for a Query.
	IdempotencyKey sdkgo.IdempotencyKey
	// Connection is the logical connection the Step used.
	Connection sdkgo.ConnectionRef
	// Input is the operation input the Step mapped.
	Input IN
	// Branch is the branch the SDK selects for the answer, defect for an
	// invalid or unscripted answer, or empty for a Retry.
	Branch sdkgo.BranchID
	// IsRetry reports that the mock asked Dex to retry the Step.
	IsRetry bool
	// IsReplay reports that a repeated idempotency key returned the first
	// terminal outcome without consuming a case.
	IsReplay bool
	// IsUnscripted reports that no case answered the call, which failed the
	// test.
	IsUnscripted bool
}

// RecordedCall is one call that any mock in the process answered, without its
// input. Test harnesses read RecordedCalls to check that every wired branch
// was exercised. The JSON field names are stable.
type RecordedCall struct {
	// ConnectorID identifies the connector manifest.
	ConnectorID string `json:"connectorId"`
	// OperationID identifies the operation within its connector.
	OperationID string `json:"operationId"`
	// ConnectionName is the logical connection name the Step used.
	ConnectionName string `json:"connectionName"`
	// FlowID is the Flow whose Step made the call.
	FlowID string `json:"flowId"`
	// CallID is the SDK's stable call identity for the Step execution.
	CallID sdkgo.CallID `json:"callId"`
	// Branch is the branch the SDK selects for the answer, or empty for a Retry.
	Branch sdkgo.BranchID `json:"branch,omitempty"`
	// IsRetry reports that the mock asked Dex to retry the Step.
	IsRetry bool `json:"retry,omitempty"`
	// IsReplay reports a repeated idempotency key answered with its first outcome.
	IsReplay bool `json:"replay,omitempty"`
	// IsUnscripted reports that no case answered the call.
	IsUnscripted bool `json:"unscripted,omitempty"`
}

var processCalls struct {
	mu    sync.Mutex
	calls []RecordedCall
}

// RecordedCalls returns a copy of every call that any Query or Mutation mock
// in this process answered, in answer order. The record lives for the whole
// process and is never reset, so a harness reads it once after its tests.
func RecordedCalls() []RecordedCall {
	processCalls.mu.Lock()
	defer processCalls.mu.Unlock()
	return append([]RecordedCall(nil), processCalls.calls...)
}

func recordProcessCall(call RecordedCall) {
	processCalls.mu.Lock()
	defer processCalls.mu.Unlock()
	processCalls.calls = append(processCalls.calls, call)
}
