// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hackernews_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hackernews/go"
	"github.com/superdurable/dex-connectors-library/connectors/hackernews/go/hackernewsmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// Binding the hand-written client fails if its Definitions drift from the generated agent metadata.
func TestClientAgentToolsClassifyProviderOutcomes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		switch r.URL.Path {
		case "/topstories.json":
			_, err = fmt.Fprint(w, `[42001,42002,42003]`)
		case "/item/42001.json":
			_, err = fmt.Fprint(w, `{"id":42001,"type":"story","by":"compilerdev","time":1793016000,"title":"A compiler release"}`)
		case "/item/42002.json":
			_, err = fmt.Fprint(w, `null`)
		default:
			w.WriteHeader(http.StatusForbidden)
		}
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := hackernews.New(hackernews.Config{BaseURL: server.URL}, nil)
	require.NoError(t, err)
	connection, err := hackernews.NewConnection(client, sdkgo.ConnectionRef{Provider: "hacker-news", Name: "public"})
	require.NoError(t, err)

	tools, err := connection.AgentTools()
	require.NoError(t, err)
	require.Equal(t, []string{"hacker-news-daily_list_story_ids", "hacker-news-daily_get_item"}, agentToolNames(tools))
	for _, tool := range tools {
		require.Equal(t, sdkgo.AgentToolEffectRead, tool.Metadata().Effect())
		require.Equal(t, connection.Reference(), tool.Connection())
	}
	listStoryIDs, getItem := tools[0], tools[1]
	// Empty feed and zero limit select defaults, so agents may omit both.
	require.Equal(t, []string(nil), agentToolRequiredInputs(t, listStoryIDs))
	require.Equal(t, []string{"id"}, agentToolRequiredInputs(t, getItem))

	attempts := []struct {
		name      string
		tool      sdkgo.AgentTool
		arguments string
		outcome   sdkgo.AgentToolAttemptOutcome
		branch    sdkgo.BranchID
	}{
		{"listStoryIDs listed", listStoryIDs, `{"feed":"top","limit":2}`, sdkgo.AgentToolAttemptSucceeded, hackernews.ListStoryIDsBranchListed},
		{"listStoryIDs defaults", listStoryIDs, `{}`, sdkgo.AgentToolAttemptSucceeded, hackernews.ListStoryIDsBranchListed},
		{"listStoryIDs limit only", listStoryIDs, `{"limit":2}`, sdkgo.AgentToolAttemptSucceeded, hackernews.ListStoryIDsBranchListed},
		{"listStoryIDs providerRejected", listStoryIDs, `{"feed":"best","limit":2}`, sdkgo.AgentToolAttemptRejected, hackernews.ListStoryIDsBranchProviderRejected},
		{"getItem found", getItem, `{"id":42001}`, sdkgo.AgentToolAttemptSucceeded, hackernews.GetItemBranchFound},
		{"getItem notFound", getItem, `{"id":42002}`, sdkgo.AgentToolAttemptRejected, hackernews.GetItemBranchNotFound},
	}
	for _, expected := range attempts {
		t.Run(expected.name, func(t *testing.T) {
			attempt, err := expected.tool.RunAttempt(singledispatchtest.NewContext("agent-host", expected.name), json.RawMessage(expected.arguments))
			require.NoError(t, err)
			require.Equal(t, expected.outcome, attempt.Outcome)
			require.Equal(t, expected.branch, attempt.Branch)
			require.True(t, attempt.HasInvokedOperation)
		})
	}

	attempt, err := listStoryIDs.RunAttempt(singledispatchtest.NewContext("agent-host", "listed-value"), json.RawMessage(`{"feed":"top","limit":2}`))
	require.NoError(t, err)
	var storyIDs hackernews.StoryIDs
	require.NoError(t, json.Unmarshal(attempt.Value, &storyIDs))
	require.Equal(t, []int64{42001, 42002}, storyIDs.IDs)
	require.True(t, storyIDs.HasMore)
}

func TestMockConnectionAgentToolsRunScriptedBranches(t *testing.T) {
	mock := hackernewsmock.New(t, "mock")
	mock.GetItem().Respond(hackernewsmock.GetItemNotFound(hackernews.Item{}, &sdkgo.Failure{Kind: sdkgo.FailureNotFound, Provider: "hacker-news", Operation: "getItem", Message: "Hacker News item is deleted or dead"}))

	tools, err := mock.Connection().AgentTools()
	require.NoError(t, err)
	require.Equal(t, agentToolNames(tools), agentToolMetadataNames(hackernews.ExposedAgentTools()))

	attempt, err := tools[0].RunAttempt(singledispatchtest.NewContext("agent-host", "mock-list"), nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptSucceeded, attempt.Outcome)

	attempt, err = tools[1].RunAttempt(singledispatchtest.NewContext("agent-host", "mock-get"), json.RawMessage(`{"id":42002}`))
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptRejected, attempt.Outcome)
	require.Equal(t, hackernews.GetItemBranchNotFound, attempt.Branch)
}

func agentToolNames(tools []sdkgo.AgentTool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Metadata().ToolName)
	}
	return names
}

func agentToolMetadataNames(metadata []sdkgo.AgentToolMetadata) []string {
	names := make([]string, 0, len(metadata))
	for _, tool := range metadata {
		names = append(names, tool.ToolName)
	}
	return names
}

func agentToolRequiredInputs(t *testing.T, tool sdkgo.AgentTool) []string {
	t.Helper()
	var schema struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(tool.Metadata().InputSchema, &schema))
	return schema.Required
}
