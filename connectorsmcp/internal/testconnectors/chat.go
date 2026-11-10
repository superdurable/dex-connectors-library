// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package testconnectors

import (
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// Identity of the chat connector, whose inputs equal the notes writes' inputs.
const (
	ChatConnectorID   = "example-chat"
	ChatModuleVersion = "v0.1.0"
)

var sendMessageDefinition = sdkgo.MutationDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: ChatConnectorID, OperationID: "sendMessage"},
	Branches: []sdkgo.BranchDefinition{
		{ID: "sent", Description: "The message was sent."},
		{ID: sdkgo.DefectBranchID, Description: "The call was invalid.", Optional: true},
	},
	StepDefaults: sdkgo.StepDefaults{ExecuteMethodTimeout: 10 * time.Second},
}

var setTopicDefinition = sdkgo.MutationDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: ChatConnectorID, OperationID: "setTopic"},
	Branches: []sdkgo.BranchDefinition{
		{ID: "set", Description: "The topic was set."},
		{ID: sdkgo.DefectBranchID, Description: "The call was invalid.", Optional: true},
	},
	StepDefaults: sdkgo.StepDefaults{ExecuteMethodTimeout: 10 * time.Second},
}

// ChatSends counts every chat write that ran, across connections.
type ChatSends struct{ count atomic.Int64 }

// Count returns how many chat writes ran.
func (sends *ChatSends) Count() int64 { return sends.count.Load() }

// ChatConnector links an in-memory chat connector whose writes count into sends.
func ChatConnector(sends *ChatSends) registry.LinkedConnector {
	return registry.LinkedConnector{
		ConnectorID: ChatConnectorID, DisplayName: "Example Chat", ModulePath: "example.com/connectors/example-chat/go",
		ModuleVersion: ChatModuleVersion, ExposedAgentTools: chatExposedAgentTools(),
		OpenAgentTools: func(_ *projectconfig.LoadedProject, connectionName string) ([]sdkgo.AgentTool, error) {
			reference := sdkgo.ConnectionRef{Provider: ChatConnectorID, Name: connectionName}
			metadata := chatExposedAgentTools()
			sendMessage, err := sdkgo.NewMutationAgentTool[PostNoteInput, PostedNote](chatMutation[PostNoteInput, PostedNote]{definition: sendMessageDefinition, branch: "sent", sends: sends}, reference, metadata[0])
			if err != nil {
				return nil, err
			}
			setTopic, err := sdkgo.NewMutationAgentTool[SaveRecordInput, SavedRecord](chatMutation[SaveRecordInput, SavedRecord]{definition: setTopicDefinition, branch: "set", sends: sends}, reference, metadata[1])
			if err != nil {
				return nil, err
			}
			return []sdkgo.AgentTool{sendMessage, setTopic}, nil
		},
	}
}

// ChatConnection returns a chat connection record for connections.json.
func ChatConnection(connectionName string) ConnectionsFileConnection {
	return ConnectionsFileConnection{ConnectorID: ChatConnectorID, ConnectionName: connectionName, Configuration: map[string]any{}, Credentials: map[string]any{}}
}

func chatExposedAgentTools() []sdkgo.AgentToolMetadata {
	return []sdkgo.AgentToolMetadata{
		{
			Operation: sendMessageDefinition.Operation, Kind: sdkgo.AgentOperationKindMutation,
			ToolName: ChatConnectorID + "_send_message", Description: "Send a chat message.",
			InputType: "PostNoteInput", InputSchema: json.RawMessage(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"}}}`),
			Branches: sendMessageDefinition.Branches, StepDefaults: sendMessageDefinition.StepDefaults,
		},
		{
			Operation: setTopicDefinition.Operation, Kind: sdkgo.AgentOperationKindMutation, IsRetrySafe: true,
			ToolName: ChatConnectorID + "_set_topic", Description: "Set the chat topic.",
			InputType: "SaveRecordInput", InputSchema: json.RawMessage(`{"type":"object","required":["name","value"],"properties":{"name":{"type":"string"},"value":{"type":"string"}}}`),
			Branches: setTopicDefinition.Branches, StepDefaults: setTopicDefinition.StepDefaults,
		},
	}
}

type chatMutation[IN, OUT any] struct {
	definition sdkgo.MutationDefinition
	branch     sdkgo.BranchID
	sends      *ChatSends
}

func (mutation chatMutation[IN, OUT]) Definition() sdkgo.MutationDefinition {
	return mutation.definition
}

func (chatMutation[IN, OUT]) IdempotencyKey(id sdkgo.CallID, _ IN) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (mutation chatMutation[IN, OUT]) Invoke(sdkgo.Call, IN) sdkgo.MutationAttempt[OUT] {
	mutation.sends.count.Add(1)
	var output OUT
	return sdkgo.NewMutationBranch(mutation.branch, output, nil, sdkgo.Receipt{})
}
