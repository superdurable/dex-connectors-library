// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/front/examples/conversation-triage/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestNewLoggerUsesLogLevelAndWarnsAboutAnInvalidOne(t *testing.T) {
	var output bytes.Buffer
	require.True(t, newLogger(&output, "debug").Enabled(context.Background(), slog.LevelDebug))
	require.False(t, newLogger(&output, "").Enabled(context.Background(), slog.LevelDebug))
	newLogger(&output, "verbose")
	require.Contains(t, output.String(), "LOG_LEVEL is not debug, info, warn, or error")
}

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("FRONT_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("FRONT_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("FRONT_EXAMPLE_MISSING", "fallback"))
}

func TestLocalAPIOptionsRedirectOnlyWhenTheLocalVariableIsSet(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	t.Setenv(localAPIBaseURLEnvironmentVariable, "")
	require.Empty(t, localAPIOptions(logger))
	t.Setenv(localAPIBaseURLEnvironmentVariable, "http://127.0.0.1:8899")
	require.Len(t, localAPIOptions(logger), 1)
}

func TestStepConfigurationsLoadTheSavedPicksOrEmptyOnes(t *testing.T) {
	searchReference, routingReference := conversationtriage.SearchConfigurationRef(), conversationtriage.RoutingConfigurationRef()
	configuration := projectconfig.Configuration{OperationConfigurations: []projectconfig.OperationConfiguration{
		{
			ConnectorID: searchReference.ConnectorID, ConnectionName: searchReference.ConnectionName, OperationID: searchReference.OperationID,
			FlowType: searchReference.FlowType, StepType: searchReference.StepType, Configuration: json.RawMessage(`{"inboxId":"` + fakeInboxID + `"}`),
		},
		{
			ConnectorID: routingReference.ConnectorID, ConnectionName: routingReference.ConnectionName, OperationID: routingReference.OperationID,
			FlowType: routingReference.FlowType, StepType: routingReference.StepType,
			Configuration: json.RawMessage(`{"assigneeId":"` + fakeTeammateID + `","tagId":"` + fakeTriageTagID + `"}`),
		},
	}}
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	search, err := loadSearchConfiguration(configuration)
	require.NoError(t, err)
	require.Equal(t, fakeInboxID, search.Value.InboxID)
	routing, err := loadRoutingConfiguration(configuration, logger)
	require.NoError(t, err)
	require.Equal(t, conversationtriage.RoutingConfiguration{AssigneeID: fakeTeammateID, TagID: fakeTriageTagID}, routing.Value)
	require.Empty(t, output.String())

	search, err = loadSearchConfiguration(projectconfig.Configuration{})
	require.NoError(t, err)
	require.Equal(t, searchReference, search.Reference)
	require.Empty(t, search.Value.InboxID, "an unsaved inboxPicker searches every inbox")
	routing, err = loadRoutingConfiguration(projectconfig.Configuration{}, logger)
	require.NoError(t, err)
	require.Empty(t, routing.Value.TagID, "an unsaved tagPicker fails each Flow with guidance instead of stopping the Worker")
	require.Contains(t, output.String(), "the triage tag is not configured")
}

func TestTriageHelpersChooseTheRequesterAndWriteTheComment(t *testing.T) {
	details := front.ConversationDetails{
		Conversation: front.Conversation{Recipient: &front.Recipient{Handle: "fallback@acme.example.com", Role: "to"}},
		Messages: []front.Message{
			{IsInbound: false, Recipients: []front.Recipient{{Handle: "support@company.example.com", Role: "from"}}},
			{IsInbound: true, Recipients: []front.Recipient{{Handle: "+12345678900", Role: "from"}}},
			{IsInbound: true, Recipients: []front.Recipient{{Handle: "jane@acme.example.com", Role: "to"}, {Handle: "jane@acme.example.com", Role: "from"}}},
		},
	}
	require.Equal(t, "jane@acme.example.com", conversationtriage.ChooseRequesterEmail(details))
	details.Messages = nil
	require.Equal(t, "fallback@acme.example.com", conversationtriage.ChooseRequesterEmail(details))
	details.Conversation.Recipient = &front.Recipient{Handle: "+12345678900"}
	require.Empty(t, conversationtriage.ChooseRequesterEmail(details))

	require.False(t, conversationtriage.HasTriageComment(details))
	details.Comments = []front.Comment{{Body: "Checking billing"}, {Body: conversationtriage.TriageCommentPrefix + " done"}}
	require.True(t, conversationtriage.HasTriageComment(details))

	require.Equal(t, "Dex triage: the requester jane@acme.example.com has no Front contact and no other open conversation.",
		conversationtriage.BuildTriageComment("jane@acme.example.com", "", nil, false))
	require.Equal(t, "Dex triage: the requester jane@acme.example.com is Front contact crd_1 and has 1 other open conversation: cnv_2.",
		conversationtriage.BuildTriageComment("jane@acme.example.com", "crd_1", []string{"cnv_2"}, false))
	many := []string{"cnv_1", "cnv_2", "cnv_3", "cnv_4", "cnv_5", "cnv_6", "cnv_7", "cnv_8", "cnv_9", "cnv_10", "cnv_11"}
	require.Contains(t, conversationtriage.BuildTriageComment("jane@acme.example.com", "", many, false), "has 11 other open conversations, including: cnv_1, ")
	require.NotContains(t, conversationtriage.BuildTriageComment("jane@acme.example.com", "", many, false), "cnv_11")
}

func TestTriageCommentCountsOnlyWhatTheSearchPageProves(t *testing.T) {
	const triagedID = "cnv_0"
	conversations := []front.Conversation{{ID: "cnv_1"}, {ID: triagedID}, {ID: "cnv_1"}}
	for index := 2; index <= 10; index++ {
		conversations = append(conversations, front.Conversation{ID: fmt.Sprintf("cnv_%d", index)})
	}
	page := front.SearchConversationsOutput{Conversations: conversations, TotalCount: 30, NextPageToken: "pt_2"}
	others, hasMore := conversationtriage.ListOtherOpenConversations(page, triagedID)
	require.Len(t, others, 10, "the triaged conversation and a repeated ID are dropped")
	require.True(t, hasMore)
	comment := conversationtriage.BuildTriageComment("jane@acme.example.com", "", others, hasMore)
	require.Contains(t, comment, "has at least 10 other open conversations, including: cnv_1, ", "30 matches never read as 10")
	require.Contains(t, comment, "cnv_10.")

	page.NextPageToken = ""
	others, hasMore = conversationtriage.ListOtherOpenConversations(page, triagedID)
	require.False(t, hasMore)
	require.Contains(t, conversationtriage.BuildTriageComment("jane@acme.example.com", "", others, hasMore), "has 10 other open conversations: cnv_1, ")

	require.Contains(t, conversationtriage.BuildTriageComment("jane@acme.example.com", "", []string{"cnv_1"}, true),
		"has at least 1 other open conversation, including: cnv_1.")
	require.Contains(t, conversationtriage.BuildTriageComment("jane@acme.example.com", "", []string{}, true), "more results exist")
}
