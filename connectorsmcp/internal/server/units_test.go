// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/attemptcontext"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/registry"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestToolNamesFollowDecisionJ(t *testing.T) {
	require.Equal(t, "hacker-news-daily_list_story_ids", toolName("hacker-news-daily", "default", "list_story_ids", false))
	require.Equal(t, "slack_work__post_thread_reply", toolName("slack", "work", "post_thread_reply", true))
	require.Equal(t, "slack_my_team_x__post_thread_reply", toolName("slack", "my team/x", "post_thread_reply", true))
	require.Equal(t, "gmail_a-b_C__get_message", toolName("gmail", "a-b_C", "get_message", true))

	long := toolName("google-youtube", "a-very-long-connection-name", "list_playlist_items_for_channel", true)
	require.Len(t, long, 64)
	full := "google-youtube_a-very-long-connection-name__list_playlist_items_for_channel"
	require.Equal(t, full[:55]+"_", long[:56])
	require.Equal(t, long, toolName("google-youtube", "a-very-long-connection-name", "list_playlist_items_for_channel", true), "stable")
	require.NotEqual(t, long, toolName("google-youtube", "a-very-long-connection-name", "list_playlist_items_for_channels", true))
}

func TestOperationNamesMatchTheSharedSnakeCaseVectors(t *testing.T) {
	contents, err := os.ReadFile("../../../sdkgo/testdata/agent_tool_names.json")
	require.NoError(t, err)
	var document struct {
		Vectors []struct {
			OperationID string `json:"operationId"`
			SnakeCase   string `json:"snakeCase"`
		} `json:"vectors"`
	}
	require.NoError(t, json.Unmarshal(contents, &document))
	require.NotEmpty(t, document.Vectors)
	for _, vector := range document.Vectors {
		definition := sdkgo.QueryDefinition{
			Operation: sdkgo.OperationRef{ConnectorID: "hacker-news-daily", OperationID: vector.OperationID},
			Branches:  []sdkgo.BranchDefinition{{ID: "done", Description: "Done."}, {ID: sdkgo.DefectBranchID, Description: "Defect.", Optional: true}},
		}
		metadata := sdkgo.AgentToolMetadata{
			Operation: definition.Operation, Kind: sdkgo.AgentOperationKindQuery, ToolName: "hacker-news-daily_" + vector.SnakeCase,
			Description: "Read.", InputSchema: json.RawMessage(`{"type":"object"}`), Branches: definition.Branches,
		}
		tool, err := sdkgo.NewQueryAgentTool[struct{}, struct{}](stubQuery{definition: definition}, sdkgo.ConnectionRef{Provider: "hn", Name: "default"}, metadata)
		require.NoError(t, err, "sdkgo accepts the vector %s", vector.OperationID)
		require.Equal(t, vector.SnakeCase, registry.OperationName(tool.Metadata()), vector.OperationID)
	}
}

func TestConnectionFlagsParseTheDexCLIShape(t *testing.T) {
	entry, err := ParseConnectionFlag("slack/work")
	require.NoError(t, err)
	require.Equal(t, SelectedConnection{ConnectorID: "slack", ConnectionName: "work"}, entry)
	entry, err = ParseConnectionFlag("slack/personal=post_thread_reply,listThreadMessages")
	require.NoError(t, err)
	require.Equal(t, []string{"post_thread_reply", "listThreadMessages"}, entry.Tools)
	entry, err = ParseConnectionFlag("slack/team/a=b")
	require.NoError(t, err)
	require.Equal(t, "team/a", entry.ConnectionName)
	_, err = ParseConnectionFlag("slack/team/a=")
	require.ErrorContains(t, err, `"slack/team/a="`, "an empty tool list fails closed and names the entry")
	for _, malformed := range []string{"slack", "slack/a,b", "slack/a=x,,y"} {
		_, err := ParseConnectionFlag(malformed)
		require.Error(t, err, malformed)
	}
	_, err = NewConnectionSelection([]SelectedConnection{{ConnectorID: "slack", ConnectionName: "a"}, {ConnectorID: "slack", ConnectionName: "a"}})
	require.ErrorContains(t, err, "listed twice")
	_, err = NewConnectionSelection([]SelectedConnection{{ConnectorID: "Slack", ConnectionName: "a"}})
	require.Error(t, err)
	_, err = NewConnectionSelection([]SelectedConnection{{ConnectorID: "slack", ConnectionName: " "}})
	require.Error(t, err)
}

func TestConnectionsConfigFileParsing(t *testing.T) {
	selection, err := ParseConnectionsConfig([]byte(`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[
		{"connection":"slack/work"},{"connection":"slack/personal","tools":["post_thread_reply","post_thread_reply"]},{"connection":"gmail/me","tools":null}]}`))
	require.NoError(t, err)
	require.Equal(t, []SelectedConnection{
		{ConnectorID: "gmail", ConnectionName: "me"},
		{ConnectorID: "slack", ConnectionName: "personal", Tools: []string{"post_thread_reply"}},
		{ConnectorID: "slack", ConnectionName: "work"},
	}, selection.Connections())
	require.Len(t, selection.Warnings(), 1, "a repeated tool is kept once with a warning")
	for _, malformed := range []string{
		`{"schemaVersion":"dex-connectors-mcp/connections-config/v2","connections":[]}`,
		`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[{"connection":"slack"}]}`,
		`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[{"connection":"slack/a"},{"connection":"slack/a"}]}`,
		`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[],"extra":1}`,
		`not json`,
		`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[]} {"trailing":true}`,
		`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[]} x`,
	} {
		_, err := ParseConnectionsConfig([]byte(malformed))
		require.Error(t, err, malformed)
	}
}

func TestConnectionsConfigDigestIsIdenticalForFlagsAndFile(t *testing.T) {
	var nilSelection *ConnectionSelection
	require.Nil(t, nilSelection.ConnectionsConfigDigest(), "no selection has a null digest")

	fromFile, err := ParseConnectionsConfig([]byte(`{"schemaVersion":"dex-connectors-mcp/connections-config/v1","connections":[
		{"connection":"slack/work"},
		{"connection":"slack/personal","tools":["post_thread_reply","list_thread_messages"]},
		{"connection":"hacker-news-daily/default"}]}`))
	require.NoError(t, err)
	var flagEntries []SelectedConnection
	for _, flag := range []string{"slack/personal=post_thread_reply,list_thread_messages", "hacker-news-daily/default", "slack/work"} {
		entry, err := ParseConnectionFlag(flag)
		require.NoError(t, err)
		flagEntries = append(flagEntries, entry)
	}
	fromFlags, err := NewConnectionSelection(flagEntries)
	require.NoError(t, err)
	// Computed independently with Python json.dumps(sort_keys=True, separators=(",", ":")).
	const expected = "sha256:cc6df1b1ec4df8d0eba5f081c94d9fcbce8fadfadc554d64921f4c1a13037c49"
	require.Equal(t, expected, *fromFile.ConnectionsConfigDigest())
	require.Equal(t, expected, *fromFlags.ConnectionsConfigDigest())

	htmlCharacters, err := NewConnectionSelection([]SelectedConnection{{ConnectorID: "gmail", ConnectionName: "<me>&co", Tools: []string{"getMessage"}}})
	require.NoError(t, err)
	require.Equal(t, "sha256:f1de3c41ce4fa773726099d06269c289c42c34d5416df51c33710f81c46f95d0", *htmlCharacters.ConnectionsConfigDigest(), "no HTML escaping")

	empty, err := NewConnectionSelection(nil)
	require.NoError(t, err)
	require.Equal(t, "sha256:bbdcd6b32401259c4e60d24b74c477a9c76d1010ba6fc707d1656ac59ea114a7", *empty.ConnectionsConfigDigest())

	// Go escapes U+2028 as \u2028 even without HTML escaping; dexcli computes it in Go too.
	separator, err := NewConnectionSelection([]SelectedConnection{{ConnectorID: "slack", ConnectionName: "a\u2028b"}})
	require.NoError(t, err)
	require.Equal(t, "sha256:65295181c87728372b6ca46160e7a6021f3244f88ecbd1291964e6fab1da1632", *separator.ConnectionsConfigDigest())
}

func TestDerivedCallIDEqualsTheCallIDTheOperationSees(t *testing.T) {
	operation := &callIDCapturingMutation{}
	metadata := sdkgo.AgentToolMetadata{
		Operation: operation.Definition().Operation, Kind: sdkgo.AgentOperationKindMutation, ToolName: "example-chat_post_reply",
		Description: "Post.", InputSchema: json.RawMessage(`{"type":"object"}`), Branches: operation.Definition().Branches,
	}
	tool, err := sdkgo.NewMutationAgentTool[struct{}, struct{}](operation, sdkgo.ConnectionRef{Provider: "chat", Name: "work"}, metadata)
	require.NoError(t, err)
	for _, key := range []string{"flow/turn/call-1", "another key", strings.Repeat("k", 1024)} {
		dexContext, err := attemptcontext.New(t.Context(), attemptcontext.Options{FlowID: localFlowID, StepExecutionID: key, Attempt: 1})
		require.NoError(t, err)
		_, err = tool.RunAttempt(dexContext, json.RawMessage(`{}`))
		require.NoError(t, err)
		require.Equal(t, operation.callID, deriveCallID(localFlowID, key, metadata.Operation), key)
	}
	// Frozen, and computed independently in Python: provider idempotency keys derive from it.
	require.Equal(t, sdkgo.CallID("b51bb3e0-17ad-5964-8914-6c372c13a695"), deriveCallID(localFlowID, "flow-1/turn-1/call-1", sdkgo.OperationRef{ConnectorID: "slack", OperationID: "postThreadReply"}))
}

func TestFenceCheckpointSurvivesTheRecordRoundTrip(t *testing.T) {
	recorded := json.RawMessage(nil)
	first, err := attemptcontext.New(t.Context(), attemptcontext.Options{
		FlowID: localFlowID, StepExecutionID: "key", Attempt: 1,
		OnRecordHeartbeat: func(checkpoint json.RawMessage) error { recorded = checkpoint; return nil },
	})
	require.NoError(t, err)
	marker := singledispatch.Marker{CallID: "call-1", Attempt: 1, State: singledispatch.StateNotSent}
	require.NoError(t, first.RecordHeartbeat(marker))
	var none singledispatch.Marker
	isFound, err := first.GetLastHeartbeatValue(&none)
	require.NoError(t, err)
	require.False(t, isFound, "a recorded heartbeat reaches only the next attempt")

	second, err := attemptcontext.New(t.Context(), attemptcontext.Options{FlowID: localFlowID, StepExecutionID: "key", Attempt: 2, LastHeartbeat: recorded})
	require.NoError(t, err)
	var decoded singledispatch.Marker
	isFound, err = second.GetLastHeartbeatValue(&decoded)
	require.NoError(t, err)
	require.True(t, isFound)
	require.Equal(t, marker, decoded)
}

func TestRetryDelaysFollowThePolicy(t *testing.T) {
	policy := sdkgo.DefaultConnectorStepExecuteRetryPolicy()
	require.Equal(t, time.Second, retryDelay(policy, 1, 0))
	require.Equal(t, 2*time.Second, retryDelay(policy, 2, 0))
	require.Equal(t, 30*time.Second, retryDelay(policy, 2, 30*time.Second))
	require.Equal(t, 5*time.Minute, retryDelay(policy, 30, 0))
	require.Equal(t, int32(3), attemptLimit(policy, 3))
	policy.MaximumAttempts = 2
	require.Equal(t, int32(2), attemptLimit(policy, 3))
	require.Equal(t, time.Hour, retryWindow(sdkgo.DefaultConnectorStepExecuteRetryPolicy(), 720*time.Hour))
	require.Equal(t, 24*time.Hour, retryWindow(&dex.RetryPolicy{TotalDuration: 48 * time.Hour}, 24*time.Hour))
}

type stubQuery struct{ definition sdkgo.QueryDefinition }

func (query stubQuery) Definition() sdkgo.QueryDefinition { return query.definition }

func (stubQuery) Invoke(sdkgo.Call, struct{}) sdkgo.QueryAttempt[struct{}] {
	return sdkgo.NewQueryBranch("done", struct{}{}, nil, sdkgo.Receipt{})
}

type callIDCapturingMutation struct{ callID sdkgo.CallID }

func (*callIDCapturingMutation) Definition() sdkgo.MutationDefinition {
	return sdkgo.MutationDefinition{
		Operation: sdkgo.OperationRef{ConnectorID: "example-chat", OperationID: "postReply"},
		Branches:  []sdkgo.BranchDefinition{{ID: "posted", Description: "Posted."}, {ID: sdkgo.DefectBranchID, Description: "Defect.", Optional: true}},
	}
}

func (*callIDCapturingMutation) IdempotencyKey(id sdkgo.CallID, _ struct{}) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (mutation *callIDCapturingMutation) Invoke(call sdkgo.Call, _ struct{}) sdkgo.MutationAttempt[struct{}] {
	mutation.callID = call.ID
	return sdkgo.NewMutationBranch("posted", struct{}{}, nil, sdkgo.Receipt{})
}
