// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"encoding/json"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

type PostReplyInput struct {
	Text string `json:"text"`
}

type PostReplyOutput struct {
	MessageID string `json:"messageId"`
}

var postReplyDefinition = sdkgo.MutationDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: "example-chat", OperationID: "postReply"},
	Branches: []sdkgo.BranchDefinition{
		{ID: "posted", Description: "The reply was posted."},
		{ID: "providerRejected", Description: "The provider refused the reply.", Optional: true},
		{ID: sdkgo.DefectBranchID, Description: "Local input or configuration is invalid.", Optional: true},
	},
}

type postReplyMutation struct{}

func (postReplyMutation) Definition() sdkgo.MutationDefinition { return postReplyDefinition }

func (postReplyMutation) IdempotencyKey(id sdkgo.CallID, _ PostReplyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (postReplyMutation) Invoke(call sdkgo.Call, input PostReplyInput) sdkgo.MutationAttempt[PostReplyOutput] {
	if input.Text == "" {
		failure := sdkgo.Failure{Kind: sdkgo.FailureProviderRejection, Provider: "example-chat", Operation: "postReply", Message: "reply text is empty"}
		return sdkgo.NewMutationBranch("providerRejected", PostReplyOutput{}, &failure, sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch("posted", PostReplyOutput{MessageID: "m-1"}, nil, sdkgo.Receipt{})
}

// Example_agentTools binds a Mutation to a connection as an agent
// tool and runs one attempt. Generated connector code supplies the metadata
// from connector.yaml; an agent host supplies a Step context whose Flow ID and
// Step execution ID stay the same for every attempt of one tool call.
func Example_agentTools() {
	metadata := sdkgo.AgentToolMetadata{
		Operation:   postReplyDefinition.Operation,
		Kind:        sdkgo.AgentOperationKindMutation,
		ToolName:    "example-chat_post_reply",
		Description: "Post a reply in a chat thread.",
		IsRetrySafe: true,
		BranchOutcomes: map[sdkgo.BranchID]sdkgo.AgentBranchOutcome{
			"providerRejected":   sdkgo.AgentBranchOutcomeRejected,
			sdkgo.DefectBranchID: sdkgo.AgentBranchOutcomeRejected,
		},
		InputType:    "PostReplyInput",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
		Branches:     postReplyDefinition.Branches,
		StepDefaults: postReplyDefinition.StepDefaults,
	}
	tool, err := sdkgo.NewMutationAgentTool(postReplyMutation{}, sdkgo.ConnectionRef{Provider: "example-chat", Name: "work"}, metadata)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(tool.Metadata().ToolName, tool.Metadata().Effect())

	ctx := singledispatchtest.NewContext("agent-host", "tool-call-1")
	for _, arguments := range []string{`{"text":"On it."}`, `{"text":""}`, `{"text":"hi","channel":"C1"}`} {
		attempt, err := tool.RunAttempt(ctx, json.RawMessage(arguments))
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		fmt.Println(attempt.Outcome, attempt.Branch, string(attempt.Value))
	}
	// Output:
	// example-chat_post_reply idempotent_write
	// succeeded posted {"messageId":"m-1"}
	// rejected providerRejected {"messageId":""}
	// error: agent tool arguments do not decode into the operation input: example-chat_post_reply: json: unknown field "channel"
}
