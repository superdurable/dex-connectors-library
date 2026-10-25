//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadapproval

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/slackmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestThreadApprovalExampleRunsOnTheSlackMockWithRealDex(t *testing.T) {
	mock := slackmock.New(t, ConnectionName)
	flow, harness := newSlackIntegrationHarnessWithConnection(t, mock.Connection())
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "slack-thread-approval-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	threadTimestamp := "1710000000.000100"

	_, err := harness.client.StartFlow(ctx, flow, flowID, Input{
		EventID: "Ev-root-mock", TeamID: "T0123456789", ChannelID: "C0123456789", ThreadTimestamp: threadTimestamp,
	}, dex.StartFlowOptions{})
	require.NoError(t, err)
	waitingState := waitForSlackStatus(t, ctx, harness.client, flow, flowID, StatusWaitingForReply)
	require.NotEmpty(t, waitingState.Messages)
	require.Equal(t, "Request approval for order 1042.", waitingState.Messages[0].Text)

	var reply ReplyResult
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.ReceiveThreadReply, ReceiveThreadReplyInput{
		EventID: "Ev-reply-mock", UserID: "U0456789012",
	}, &reply))
	require.True(t, reply.Accepted)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var completedState ThreadState
	require.NoError(t, result.DecodeSingleOutput(&completedState))
	require.Equal(t, StatusCompleted, completedState.Status)
	require.Equal(t, "U0456789012", completedState.ReplyUserID)

	readCalls := mock.ListThreadMessages().Calls()
	require.Len(t, readCalls, 1)
	require.Equal(t, flowID, readCalls[0].FlowID)
	require.Equal(t, threadTimestamp, readCalls[0].Input.ThreadTimestamp)
	postCalls := mock.PostThreadReply().Calls()
	require.Len(t, postCalls, 1)
	require.Equal(t, flowID, postCalls[0].FlowID)
	require.Equal(t, "Integration complete.", postCalls[0].Input.Text)
}
