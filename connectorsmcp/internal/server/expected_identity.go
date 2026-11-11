// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Expected-identity guard keys a tools/call _meta may carry (agreed with Dex).
const (
	metaKeyExpectedConnector  = "io.superdurable.connectors/expectedConnector"
	metaKeyExpectedConnection = "io.superdurable.connectors/expectedConnection"
	metaKeyExpectedOperation  = "io.superdurable.connectors/expectedOperation"
)

// expectedIdentity is the guard a caller sent; absent keys check nothing.
type expectedIdentity struct {
	values map[string]any
}

// executionIdentity is what a call would run as: connector, connection, operation.
type executionIdentity struct {
	connectorID    string
	connectionName string
	operationID    string
}

func expectedIdentityOf(meta mcp.Meta) expectedIdentity {
	values := map[string]any{}
	for _, key := range []string{metaKeyExpectedConnector, metaKeyExpectedConnection, metaKeyExpectedOperation} {
		if value, isPresent := meta[key]; isPresent {
			values[key] = value
		}
	}
	return expectedIdentity{values: values}
}

// mismatchError returns the refusal for the first present key that differs
// from actual, or nil when every present key matches.
func (expected expectedIdentity) mismatchError(actual executionIdentity) error {
	for _, check := range []struct{ key, actual string }{
		{metaKeyExpectedConnector, actual.connectorID},
		{metaKeyExpectedConnection, actual.connectionName},
		{metaKeyExpectedOperation, actual.operationID},
	} {
		value, isPresent := expected.values[check.key]
		if !isPresent {
			continue
		}
		if text, isString := value.(string); !isString || text != check.actual {
			return expectedIdentityError(fmt.Sprintf("_meta %s does not match the call, which runs as %q; nothing was sent", check.key, check.actual))
		}
	}
	return nil
}

// isComplete reports whether all three keys are present.
func (expected expectedIdentity) isComplete() bool { return len(expected.values) == 3 }

func expectedIdentityError(message string) error {
	data, err := json.Marshal(map[string]any{"_meta": map[string]any{metaKeyOutcome: outcomeRejected}})
	if err != nil {
		panic(fmt.Sprintf("encode error data: %v", err))
	}
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: message, Data: data}
}
