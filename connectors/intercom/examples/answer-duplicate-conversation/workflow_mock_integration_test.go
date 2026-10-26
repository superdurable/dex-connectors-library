//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"github.com/superdurable/dex-connectors-library/connectors/intercom/examples/answer-duplicate-conversation/flow"
	"github.com/superdurable/dex-connectors-library/connectors/intercom/intercommock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAnswerDuplicateExampleUsesIntercomMockWithRealDex(t *testing.T) {
	mock := intercommock.New(t, answerduplicate.ConnectionName)
	configuration := sdkgo.ConnectorLoadedConfiguration[answerduplicate.ReplyConfiguration]{
		Reference: answerduplicate.ReplyConfigurationRef(), Value: answerduplicate.ReplyConfiguration{AdminID: "77"},
	}
	harness := &duplicateHarness{
		flow: answerduplicate.NewFlow(mock.Connection(), configuration), serverAddress: dexAddress(),
	}
	var err error
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", unusedPort(t))
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.startWorker(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})

	outcome := harness.runDuplicate(t, "mock-answer", "2002")
	require.Equal(t, answerduplicate.OutcomeAnsweredAndClosed, outcome.Action)
	require.Equal(t, "2002", outcome.ConversationID)
	require.Equal(t, "2001", outcome.EarlierConversationID)
	require.Equal(t, intercom.ConversationStateClosed, outcome.ConversationState)
	require.False(t, outcome.WasReplyAlreadyApplied)
	require.False(t, outcome.WasCloseAlreadyApplied)
	getCalls := mock.GetConversation().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, intercom.GetConversationInput{ConversationID: "2002", LatestPartLimit: 10}, getCalls[0].Input)
	contactCalls := mock.FindContactByEmail().Calls()
	require.Len(t, contactCalls, 1)
	require.Equal(t, intercom.FindContactByEmailInput{Email: "ada@example.com", ContactLimit: 15}, contactCalls[0].Input)
	searchCalls := mock.SearchConversations().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, intercom.SearchConversationsInput{
		States:     []intercom.ConversationState{intercom.ConversationStateOpen, intercom.ConversationStateSnoozed},
		ContactIDs: []string{"contact-1"}, PageSize: 20,
	}, searchCalls[0].Input)
	replyCalls := mock.ReplyToConversation().Calls()
	require.Len(t, replyCalls, 1)
	require.Equal(t, intercom.ReplyToConversationInput{
		ConversationID: "2002", AdminID: "77", MessageType: intercom.ReplyMessageTypeComment,
		Body: "Thanks for getting in touch. We are already working on your earlier conversation with us, so we will continue there and close this one.",
	}, replyCalls[0].Input)
	closeCalls := mock.UpdateConversationState().Calls()
	require.Len(t, closeCalls, 1)
	require.Equal(t, intercom.UpdateConversationStateInput{
		ConversationID: "2002", AdminID: "77", State: intercom.ConversationStateClosed,
	}, closeCalls[0].Input)
}
