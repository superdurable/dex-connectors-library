// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var triggerRPCApplicationState = dex.DefineAttribute[string]("trigger-rpc-application-state")

type triggerRPCTestFlow struct{}

func (*triggerRPCTestFlow) Approve(
	_ dex.Context,
	event sdkgo.TriggerEvent[string],
) (*dex.RPCResult[string], error) {
	return &dex.RPCResult[string]{Output: event.Payload}, nil
}

func TestTriggerRPCProvidesDefinitionDefaultOptionsAndPersistence(t *testing.T) {
	flow := &triggerRPCTestFlow{}
	triggerRPC, err := sdkgo.NewTriggerRPC(sdkgo.TriggerRPCConfig[string, string]{
		Definition: flow.Approve, ProcessedEventIDsAttributeName: "processed-slack-event-ids",
		HandleEvent: flow.Approve,
		Options:     &dex.RPCOptions{LockAttributes: []dex.AttributeLock{dex.LockAttribute(triggerRPCApplicationState)}},
	})
	require.NoError(t, err)
	require.NotNil(t, triggerRPC.Definition())
	require.NotNil(t, triggerRPC.PersistenceAttribute())
	firstOptions := triggerRPC.DefaultOptions()
	secondOptions := triggerRPC.DefaultOptions()
	require.Len(t, firstOptions.LockAttributes, 2)
	firstOptions.LockAttributes = nil
	require.Len(t, secondOptions.LockAttributes, 2)
}
