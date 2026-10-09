//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadreply

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	"github.com/superdurable/dex-connectors-library/connectors/google/gmail/gmailmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestThreadReplyExampleRunsOnTheGmailMockWithRealDex runs the example Flow on a real Worker with the
// generated gmailmock connection instead of the Gmail API, as an application test does.
func TestThreadReplyExampleRunsOnTheGmailMockWithRealDex(t *testing.T) {
	mock := gmailmock.New(t, ConnectionName)
	flow, harness := newGmailIntegrationHarnessWithConnection(t, mock.Connection())
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	replyFilter, err := NewReplyTriggerFilter(gmail.ReplyReceivedTriggerConfiguration{
		ReplyMatcher: gmail.MessageMatcher{MessageContains: "approved", SenderEmails: []string{"sender@example.com"}},
	})
	require.NoError(t, err)
	replyTarget := sdkgo.NewDexRPCTriggerTarget(harness.client, flow.ReceiveEmailReply, replyFilter, ResolveFlowID, MapToReceiveEmailReplyInput)
	approveThread := func(threadID string) string {
		root := gmail.MessageEvent{
			PrimaryEmail: "owner@example.com", MessageID: "root-" + threadID, ThreadID: threadID,
			From: "sender@example.com", Subject: "Approval request", Snippet: "request approval",
		}
		flowID := startGmailThreadFlow(t, ctx, harness.client, flow, "root-"+threadID, root)
		waitForGmailStatus(t, ctx, harness.client, flow, flowID, StatusWaitingForReply)
		reply := sdkgo.TriggerEvent[gmail.MessageEvent]{
			ID: "reply-" + threadID, OccurredAt: time.Now().UTC(),
			Payload: gmail.MessageEvent{
				PrimaryEmail: "owner@example.com", MessageID: "reply-" + threadID, ThreadID: threadID,
				From: "sender@example.com", Subject: "Re: Approval request", Snippet: "approved", IsReply: true,
			},
		}
		require.Eventually(t, func() bool {
			err := replyTarget.HandleTrigger(ctx, reply)
			return err == nil || !strings.Contains(err.Error(), "attribute keys are locked")
		}, 20*time.Second, 50*time.Millisecond)
		return flowID
	}

	sentThreadID := "thread-mock-sent-" + testRunID
	uncertainThreadID := "thread-mock-uncertain-" + testRunID
	mock.ReplyToMessage().ForFlow(ResolveFlowID(threadEvent(sentThreadID))).Respond(gmailmock.ReplyToMessageRepliedInThread())
	mock.ReplyToMessage().ForFlow(ResolveFlowID(threadEvent(uncertainThreadID))).Respond(gmailmock.ReplyToMessageConnectionLost())

	sentFlowID := approveThread(sentThreadID)
	result, err := harness.client.WaitForFlow(ctx, sentFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var state ThreadState
	require.NoError(t, result.DecodeSingleOutput(&state))
	require.Equal(t, StatusCompleted, state.Status)
	require.Equal(t, "Re: Order 1042 has not arrived", state.RootMessage.Subject, "the manifest default answers the unscripted read")

	uncertainFlowID := approveThread(uncertainThreadID)
	uncertainResult, err := harness.client.WaitForFlow(ctx, uncertainFlowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, uncertainResult.Status, "the example leaves the optional uncertain branch unwired")
	require.Contains(t, uncertainResult.ErrorMessage, `connector branch "uncertain" has no target`)

	replies := map[string][]gmail.ReplyToMessageInput{}
	for _, call := range mock.ReplyToMessage().Calls() {
		replies[call.FlowID] = append(replies[call.FlowID], call.Input)
	}
	require.Equal(t, []gmail.ReplyToMessageInput{{MessageID: "reply-" + sentThreadID, TextBody: "Processing complete."}}, replies[sentFlowID])
	require.Len(t, replies[uncertainFlowID], 1, "an uncertain reply is reported, not resent")
}

func threadEvent(threadID string) sdkgo.TriggerEvent[gmail.MessageEvent] {
	return sdkgo.TriggerEvent[gmail.MessageEvent]{Payload: gmail.MessageEvent{ThreadID: threadID}}
}
