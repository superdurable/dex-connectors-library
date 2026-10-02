// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var replyReceivedAt = time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)

func TestReplyToMessageThreadsMarksAndSendsOneReply(t *testing.T) {
	fake := newGraphFake(t)
	original := fake.AddMessage(graphtest.SeedMessage{
		FromName: "Jane Smith", FromAddress: testCustomer, To: []string{testMailbox, "colleague@contoso.example"},
		Cc: []string{"finance@acme.example.com"}, Subject: "Refund for order 88213", Body: "Charged twice.", ReceivedAt: replyReceivedAt,
	})
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunMutation(newOutlookDexContext("reply"), client.ReplyToMessage(), outlookConnection, outlookmail.ReplyToMessageInput{
		MessageID: original, Text: "Hi Jane, the duplicate charge was refunded.",
	})
	require.NoError(t, err)
	require.Equal(t, outlookmail.ReplyToMessageBranchSent, result.Branch, result.Failure)
	require.Equal(t, "RE: Refund for order 88213", result.Value.Subject)
	require.Equal(t, []string{testCustomer}, result.Value.Recipients, "a plain reply goes to the sender only")

	replies := fake.Requests(graphtest.EndpointCreateReply)
	require.Len(t, replies, 1)
	require.Equal(t, "/v1.0/me/messages/"+original+"/createReply", replies[0].Path)
	require.JSONEq(t, `{"comment":"Hi Jane, the duplicate charge was refunded."}`, string(replies[0].Body))
	updates := fake.Requests(graphtest.EndpointUpdateMessage)
	require.Len(t, updates, 1, "the reply draft gets its marker before it is sent")
	var marker map[string]any
	require.NoError(t, json.Unmarshal(updates[0].Body, &marker))
	require.Equal(t, map[string]any{"singleValueExtendedProperties": []any{map[string]any{
		"id": outlookmail.IdempotencyMarkerPropertyID, "value": result.Value.IdempotencyMarker,
	}}}, marker)

	deliveries := fake.Deliveries()
	require.Len(t, deliveries, 1)
	sent, ok := fake.Message(result.Value.MessageID)
	require.True(t, ok)
	require.False(t, sent.IsDraft)
	originalView, _ := fake.Message(original)
	require.Equal(t, originalView.ConversationID, sent.ConversationID, "Outlook threads the reply with the original")
	require.Contains(t, sent.Body, "Charged twice.", "Outlook quotes the original below the reply")
	require.Equal(t, result.Value.IdempotencyMarker, sent.ExtendedProperties[outlookmail.IdempotencyMarkerPropertyID])
}

func TestReplyToMessageReplyAllAddressesEveryoneButTheMailbox(t *testing.T) {
	fake := newGraphFake(t)
	original := fake.AddMessage(graphtest.SeedMessage{
		FromAddress: testCustomer, To: []string{testMailbox, "colleague@contoso.example"}, Cc: []string{"finance@acme.example.com"},
		Subject: "RE: Refund", Body: "Charged twice.", ReceivedAt: replyReceivedAt,
	})
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunMutation(newOutlookDexContext("reply-all"), client.ReplyToMessage(), outlookConnection, outlookmail.ReplyToMessageInput{
		MessageID: original, Text: "Refunded.", IsReplyAll: true,
	})
	require.NoError(t, err)
	require.Equal(t, outlookmail.ReplyToMessageBranchSent, result.Branch, result.Failure)
	require.Equal(t, "RE: Refund", result.Value.Subject, "an existing RE: prefix is kept once")
	require.Equal(t, []string{testCustomer, "colleague@contoso.example", "finance@acme.example.com"}, result.Value.Recipients)
	require.Equal(t, "/v1.0/me/messages/"+original+"/createReplyAll", fake.Requests(graphtest.EndpointCreateReply)[0].Path)
}

func TestReplyToMessageMissingOriginalSelectsNotFoundWithoutDrafting(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunMutation(newOutlookDexContext("missing"), client.ReplyToMessage(), outlookConnection, outlookmail.ReplyToMessageInput{
		MessageID: "AAMkMessage-404_gone=", Text: "Refunded.",
	})
	require.NoError(t, err)
	require.Equal(t, outlookmail.ReplyToMessageBranchNotFound, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	requireNoSecretsOrServerText(t, result.Failure)
	require.Empty(t, fake.MessagesInFolder("drafts"))
	require.Zero(t, fake.RequestCount(graphtest.EndpointSendDraft))
}

func TestReplyToMessageFailedMarkerUpdateResumesTheCheckpointedDraft(t *testing.T) {
	fake := newGraphFake(t)
	original := seedCustomerMessage(fake, "Refund", replyReceivedAt)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointUpdateMessage, Status: http.StatusServiceUnavailable, RetryAfter: "2"})
	client, _ := delegatedClient(t, fake)
	first := newOutlookDexContext("marker-retry")
	input := outlookmail.ReplyToMessageInput{MessageID: original, Text: "Refunded."}
	_, err := sdkgo.RunMutation(first, client.ReplyToMessage(), outlookConnection, input)
	_, delay := requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 2*time.Second, delay)
	require.Contains(t, string(first.recordedHeartbeat), `"outlookMailSendPhase":"draftReady"`)

	result, err := sdkgo.RunMutation(first.nextAttempt(), client.ReplyToMessage(), outlookConnection, input)
	require.NoError(t, err)
	require.Equal(t, outlookmail.ReplyToMessageBranchSent, result.Branch, result.Failure)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointCreateReply), "the checkpointed draft is reused")
	require.Len(t, fake.Deliveries(), 1)
}

func TestReplyToMessageLostCreateReplyAnswerLeavesOneUnsentDraftAndSendsOnce(t *testing.T) {
	fake := newGraphFake(t)
	original := seedCustomerMessage(fake, "Refund", replyReceivedAt)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointCreateReply, ShouldApplyFirst: true, ShouldDropConnection: true})
	client, _ := delegatedClient(t, fake)
	first := newOutlookDexContext("lost-create-reply")
	input := outlookmail.ReplyToMessageInput{MessageID: original, Text: "Refunded."}
	_, err := sdkgo.RunMutation(first, client.ReplyToMessage(), outlookConnection, input)
	requireRetry(t, err, sdkgo.FailureTransport)

	result, err := sdkgo.RunMutation(first.nextAttempt(), client.ReplyToMessage(), outlookConnection, input)
	require.NoError(t, err)
	require.Equal(t, outlookmail.ReplyToMessageBranchSent, result.Branch, result.Failure)
	require.Len(t, fake.Deliveries(), 1, "createReply takes no marker, so the lost draft cannot be found, but it is never sent")
	require.Len(t, fake.MessagesInFolder("drafts"), 1, "the unmarked reply draft from the lost answer stays unsent in Drafts")
}

func TestReplyToMessageRejectsInvalidInputWithoutContactingGraph(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	for _, input := range []outlookmail.ReplyToMessageInput{
		{MessageID: "", Text: "Refunded."},
		{MessageID: "../me/sendMail", Text: "Refunded."},
		{MessageID: "AAMkMessage-1=", Text: "  "},
	} {
		result, err := sdkgo.RunMutation(newOutlookDexContext("invalid-reply"), client.ReplyToMessage(), outlookConnection, input)
		require.NoError(t, err)
		require.Equal(t, outlookmail.ReplyToMessageBranchDefect, result.Branch)
	}
	require.Zero(t, fake.RequestCount(graphtest.EndpointCreateReply)+fake.RequestCount(graphtest.EndpointFindMessagesByMarker))
}
