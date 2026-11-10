// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"encoding/binary"

	"github.com/google/uuid"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// LaunchContract is the integer dexcli keys on: it changes only when flags,
// the ready line, server.json, /healthz, or the state directory format change.
const LaunchContract = 1

// ImplementationName is the MCP server name.
const ImplementationName = "dex-connectors-mcp"

// localFlowID is the Flow ID of every attempt context. With the idempotency
// key as Step execution ID it fixes the connector Call ID, from which provider
// idempotency keys derive, so it never changes within call-state v1.
const localFlowID = "dex-connectors-mcp"

// Dex MCP convention keys (superdurable/dex agents/python/core README, "Dex MCP conventions").
const (
	metaKeyIdempotencyKey = "io.superdurable.dex/idempotencyKey"
	metaKeyEffect         = "io.superdurable.dex/effect"
	metaKeyApproval       = "io.superdurable.dex/approval"
	metaKeyOutcome        = "io.superdurable.dex/outcome"
	approvalRecommended   = "recommended"
	outcomeRejected       = "rejected"
	outcomeUnknown        = "unknown"
)

// Server-specific keys; Dex CLI runtimes filter tools on the identity keys.
const (
	metaKeyConnector        = "io.superdurable.connectors/connector"
	metaKeyConnection       = "io.superdurable.connectors/connection"
	metaKeyOperation        = "io.superdurable.connectors/operation"
	metaKeyOperationName    = "io.superdurable.connectors/operationName"
	metaKeyConnectorVersion = "io.superdurable.connectors/connectorVersion"
	metaKeyBranch           = "io.superdurable.connectors/branch"
	metaKeyReplayed         = "io.superdurable.connectors/replayed"
	metaKeyTruncatedBytes   = "io.superdurable.connectors/truncatedBytes"
)

// callIDNamespace and deriveCallID mirror sdkgo's unexported derivation; a
// test pins them to the Call ID that RunAttempt hands the operation.
var callIDNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://superdurable.dev/dex-connectors/call-id/v1"))

func deriveCallID(flowID string, stepExecutionID string, operation sdkgo.OperationRef) sdkgo.CallID {
	fields := []string{flowID, stepExecutionID, operation.ConnectorID, operation.OperationID}
	payload := make([]byte, 0, len(flowID)+len(stepExecutionID)+len(operation.ConnectorID)+len(operation.OperationID)+16)
	var length [4]byte
	for _, field := range fields {
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		payload = append(payload, length[:]...)
		payload = append(payload, field...)
	}
	return sdkgo.CallID(uuid.NewSHA1(callIDNamespace, payload).String())
}
