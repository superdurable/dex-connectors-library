// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/go/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validSendMessageInput() outlookmail.SendMessageInput {
	return outlookmail.SendMessageInput{
		To: []string{testCustomer}, Cc: []string{"finance@acme.example.com"}, Bcc: []string{"audit@contoso.example"},
		ReplyTo: []string{"billing@contoso.example"}, Subject: "Ihre Rückerstattung für Bestellung 88213",
		Text: "Hi Jane,\n\nThe duplicate charge was refunded.",
	}
}

func TestSendMessageCreatesOneMarkedDraftAndSendsItOnce(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	ctx := newOutlookDexContext("send")
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), outlookConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, outlookmail.SendMessageBranchSent, result.Branch, result.Failure)
	marker := outlookmail.IdempotencyMarkerPrefix + string(result.Receipt.IdempotencyKey)
	require.Equal(t, string(result.Receipt.CallID), string(result.Receipt.IdempotencyKey))
	require.Equal(t, marker, result.Value.IdempotencyMarker)
	require.False(t, result.Value.WasAlreadySent)
	require.Equal(t, []string{testCustomer, "finance@acme.example.com", "audit@contoso.example"}, result.Value.Recipients)
	require.Equal(t, "Ihre Rückerstattung für Bestellung 88213", result.Value.Subject)
	require.NotEmpty(t, result.Value.InternetMessageID)
	require.Equal(t, result.Value.MessageID, result.Receipt.ProviderObjectID)

	created := fake.Requests(graphtest.EndpointCreateDraft)
	require.Len(t, created, 1)
	var draft map[string]any
	require.NoError(t, json.Unmarshal(created[0].Body, &draft))
	require.Equal(t, map[string]any{"contentType": "text", "content": "Hi Jane,\n\nThe duplicate charge was refunded."}, draft["body"])
	require.Equal(t, []any{map[string]any{"id": outlookmail.IdempotencyMarkerPropertyID, "value": marker}}, draft["singleValueExtendedProperties"],
		"the marker is created with the draft")
	require.Equal(t, []any{map[string]any{"emailAddress": map[string]any{"name": "", "address": "billing@contoso.example"}}}, draft["replyTo"])
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointFindMessagesByMarker), "the first attempt looks for its own draft first")
	sends := fake.Requests(graphtest.EndpointSendDraft)
	require.Len(t, sends, 1)
	require.Equal(t, "0", sends[0].Header.Get("Content-Length"), "Graph's send action needs Content-Length: 0")
	require.Contains(t, sends[0].Header.Values("Prefer"), `IdType="ImmutableId"`)
	require.JSONEq(t, `{"outlookMailSendPhase":"sendDispatched","outlookMailDraftMessageId":"`+result.Value.MessageID+`","outlookMailSendCount":1}`, string(ctx.recordedHeartbeat),
		"the dispatch checkpoint is recorded before the send")

	deliveries := fake.Deliveries()
	require.Len(t, deliveries, 1)
	sent, ok := fake.Message(result.Value.MessageID)
	require.True(t, ok, "the immutable draft ID names the Sent Items copy")
	require.False(t, sent.IsDraft)
	require.Equal(t, fake.FolderID("sentitems"), sent.FolderID)
	require.Equal(t, marker, sent.ExtendedProperties[outlookmail.IdempotencyMarkerPropertyID])
}

func TestSendMessageLookupFilterNamesTheMarkerProperty(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunMutation(newOutlookDexContext("filter"), client.SendMessage(), outlookConnection, validSendMessageInput())
	require.NoError(t, err)
	lookup := fake.Requests(graphtest.EndpointFindMessagesByMarker)[0]
	require.Equal(t, "/v1.0/me/messages", lookup.Path)
	require.Equal(t, "singleValueExtendedProperties/Any(ep: ep/id eq '"+outlookmail.IdempotencyMarkerPropertyID+"' and ep/value eq '"+
		result.Value.IdempotencyMarker+"')", lookup.Query.Get("$filter"))
}

func TestSendMessageAfterALostCreateAnswerReusesTheMarkedDraft(t *testing.T) {
	fake := newGraphFake(t)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointCreateDraft, ShouldApplyFirst: true, ShouldDropConnection: true})
	client, _ := delegatedClient(t, fake)
	first := newOutlookDexContext("lost-create")
	_, err := sdkgo.RunMutation(first, client.SendMessage(), outlookConnection, validSendMessageInput())
	message, _ := requireRetry(t, err, sdkgo.FailureTransport)
	require.NotContains(t, message, graphtest.SentinelText)
	require.Nil(t, first.recordedHeartbeat, "no draft ID was learned, so no checkpoint is recorded")

	result, err := sdkgo.RunMutation(first.nextAttempt(), client.SendMessage(), outlookConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, outlookmail.SendMessageBranchSent, result.Branch, result.Failure)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointCreateDraft), "the retry found the draft by its marker instead of creating another")
	require.Len(t, fake.Deliveries(), 1)
	require.Empty(t, fake.MessagesInFolder("drafts"))
}

func TestSendMessageThrottledSendIsRetriedWithTheSameDraft(t *testing.T) {
	fake := newGraphFake(t)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, Status: http.StatusTooManyRequests, ErrorCode: "ApplicationThrottled", RetryAfter: "7"})
	client, _ := delegatedClient(t, fake)
	first := newOutlookDexContext("throttled")
	_, err := sdkgo.RunMutation(first, client.SendMessage(), outlookConnection, validSendMessageInput())
	message, delay := requireRetry(t, err, sdkgo.FailureRateLimit)
	require.Equal(t, "Microsoft Graph throttled the request (HTTP 429 ApplicationThrottled)", message)
	require.Equal(t, 7*time.Second, delay)
	require.Contains(t, string(first.recordedHeartbeat), `"outlookMailSendPhase":"draftReady"`, "a 429 provably sent nothing, so the draft is ready again")

	result, err := sdkgo.RunMutation(first.nextAttempt(), client.SendMessage(), outlookConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, outlookmail.SendMessageBranchSent, result.Branch, result.Failure)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointCreateDraft))
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointFindMessagesByMarker), "the checkpointed draft is read by ID, not looked up")
	require.Equal(t, 2, fake.RequestCount(graphtest.EndpointSendDraft))
	require.Len(t, fake.Deliveries(), 1)
}

func TestSendMessageLostSendAnswerIsConfirmedFromSentItemsWithoutResending(t *testing.T) {
	fake := newGraphFake(t)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, ShouldApplyFirst: true, ShouldDropConnection: true})
	client, _ := delegatedClient(t, fake)
	first := newOutlookDexContext("lost-send")
	_, err := sdkgo.RunMutation(first, client.SendMessage(), outlookConnection, validSendMessageInput())
	message, delay := requireRetry(t, err, sdkgo.FailureTransport)
	require.Contains(t, message, "a later attempt reads the draft back before sending again")
	require.Equal(t, 3*time.Second, delay, "Exchange needs a moment to move a sent draft")

	result, err := sdkgo.RunMutation(first.nextAttempt(), client.SendMessage(), outlookConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, outlookmail.SendMessageBranchSent, result.Branch, result.Failure)
	require.True(t, result.Value.WasAlreadySent)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointSendDraft), "the confirmation never sends again")
	require.Len(t, fake.Deliveries(), 1)
}

func TestSendMessageCopyThatNeverAppearsIsResentUntilTheSendCapThenUncertain(t *testing.T) {
	fake := newGraphFake(t)
	fake.SetSendCompletionDelay(time.Hour)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, Count: 3, ShouldApplyFirst: true, Status: http.StatusGatewayTimeout})
	client, _ := delegatedClient(t, fake)
	attempt := newOutlookDexContext("slow-copy")
	_, err := sdkgo.RunMutation(attempt, client.SendMessage(), outlookConnection, validSendMessageInput())
	message, _ := requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, "Microsoft Graph failed or is temporarily unavailable (HTTP 504 UnknownError); the send was dispatched, "+
		"so a later attempt reads the draft back before sending again", message)

	for sendCount := 2; sendCount <= 3; sendCount++ {
		attempt = attempt.nextAttempt()
		_, err = sdkgo.RunMutation(attempt, client.SendMessage(), outlookConnection, validSendMessageInput())
		_, delay := requireRetry(t, err, sdkgo.FailureAvailability)
		require.Equal(t, 3*time.Second, delay)
		require.Contains(t, string(attempt.recordedHeartbeat), `"outlookMailSendCount":`+string(rune('0'+sendCount)))
	}
	result, err := sdkgo.RunMutation(attempt.nextAttempt(), client.SendMessage(), outlookConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, outlookmail.SendMessageBranchUncertain, result.Branch, "the read-back after the third send found only the draft")
	require.Equal(t, 3, fake.RequestCount(graphtest.EndpointSendDraft), "the default send cap is three")
	require.Equal(t, 3, fake.RequestCount(graphtest.EndpointGetMessage), "every attempt after a send reads the draft back first")
	require.NotEmpty(t, result.Value.MessageID, "the uncertain Result names the draft to inspect")
	require.Equal(t, []string{testCustomer, "finance@acme.example.com", "audit@contoso.example"}, result.Value.Recipients)
	requireNoSecretsOrServerText(t, result.Failure)
}

func TestSendMessageSendCapOneSelectsUncertainAfterTheFirstUnconfirmedSend(t *testing.T) {
	fake := newGraphFake(t)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, ShouldDropConnection: true})
	client, _ := delegatedClient(t, fake)
	attempt := newOutlookDexContext("cap-one")
	_, err := sdkgo.RunMutation(attempt, client.SendMessage(), outlookConnection, validSendMessageInput(), sdkgo.WithMaxSends(1))
	requireRetry(t, err, sdkgo.FailureTransport)
	result, err := sdkgo.RunMutation(attempt.nextAttempt(), client.SendMessage(), outlookConnection, validSendMessageInput(), sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, outlookmail.SendMessageBranchUncertain, result.Branch)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointSendDraft))
	require.Len(t, fake.MessagesInFolder("drafts"), 1, "the draft stays for a person to inspect")
}

func TestSendMessageDispatchedSendIsConfirmedWhenTheCopyAppears(t *testing.T) {
	fake := newGraphFake(t)
	fake.SetSendCompletionDelay(time.Hour)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, ShouldApplyFirst: true, ShouldDropConnection: true})
	client, _ := delegatedClient(t, fake)
	first := newOutlookDexContext("copy-appears")
	_, err := sdkgo.RunMutation(first, client.SendMessage(), outlookConnection, validSendMessageInput())
	requireRetry(t, err, sdkgo.FailureTransport)

	draftID := fake.MessagesInFolder("drafts")[0].ID
	fake.CompletePendingSends()
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.SendMessage(), outlookConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, outlookmail.SendMessageBranchSent, result.Branch, result.Failure)
	require.True(t, result.Value.WasAlreadySent)
	require.Equal(t, draftID, result.Value.MessageID)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointSendDraft), "the read-back confirmed the send, so it is not sent again")
	require.Len(t, fake.Deliveries(), 1)
}

func TestSendMessageDroppedAnswerThatNeverSentIsResentAfterReadBack(t *testing.T) {
	fake := newGraphFake(t)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, ShouldDropConnection: true})
	client, _ := delegatedClient(t, fake)
	attempt := newOutlookDexContext("never-sent")
	_, err := sdkgo.RunMutation(attempt, client.SendMessage(), outlookConnection, validSendMessageInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	result, err := sdkgo.RunMutation(attempt.nextAttempt(), client.SendMessage(), outlookConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, outlookmail.SendMessageBranchSent, result.Branch, result.Failure)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointGetMessage), "the draft is read back before the resend")
	require.Equal(t, 2, fake.RequestCount(graphtest.EndpointSendDraft))
	require.Len(t, fake.Deliveries(), 1)
}

func TestSendMessageRejectedRecipientSendsNothingWithoutServerText(t *testing.T) {
	fake := newGraphFake(t)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, Status: http.StatusBadRequest, ErrorCode: "ErrorInvalidRecipients"})
	client, _ := delegatedClient(t, fake)
	ctx := newOutlookDexContext("rejected")
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), outlookConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, outlookmail.SendMessageBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.Equal(t, "Microsoft Graph rejected the request (HTTP 400 ErrorInvalidRecipients); nothing was sent and the draft stays in Drafts", result.Failure.Message)
	requireNoSecretsOrServerText(t, result.Failure)
	require.Empty(t, fake.Deliveries())
	require.Contains(t, string(ctx.recordedHeartbeat), `"outlookMailSendPhase":"draftReady"`, "the refusal releases the dispatch before the branch")
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointSendDraft))
}

func TestSendMessageRefusalAfterAnUnconfirmedSendIsReadBackNotRejected(t *testing.T) {
	fake := newGraphFake(t)
	fake.SetSendCompletionDelay(time.Hour)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, ShouldApplyFirst: true, Status: http.StatusBadGateway})
	client, _ := delegatedClient(t, fake)
	attempt := newOutlookDexContext("refusal-after-unknown")
	_, err := sdkgo.RunMutation(attempt, client.SendMessage(), outlookConnection, validSendMessageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	fake.InjectFault(graphtest.Fault{Endpoint: graphtest.EndpointSendDraft, Status: http.StatusForbidden, ErrorCode: "ErrorAccessDenied"})
	attempt = attempt.nextAttempt()
	_, err = sdkgo.RunMutation(attempt, client.SendMessage(), outlookConnection, validSendMessageInput())
	message, _ := requireRetry(t, err, sdkgo.FailureAuthorization)
	require.Contains(t, message, "an earlier send may have delivered the draft")
	fake.CompletePendingSends()
	result, err := sdkgo.RunMutation(attempt.nextAttempt(), client.SendMessage(), outlookConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, outlookmail.SendMessageBranchSent, result.Branch, result.Failure)
	require.True(t, result.Value.WasAlreadySent)
	require.Equal(t, 2, fake.RequestCount(graphtest.EndpointSendDraft))
}

func TestSendMessageUnreadableCheckpointRetriesWithoutContactingGraph(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	earlier := newOutlookDexContext("unreadable")
	earlier.recordedHeartbeat = []byte(`{"emailSubmittedCallId":"another connector"}`)
	_, err := sdkgo.RunMutation(earlier.nextAttempt(), client.SendMessage(), outlookConnection, validSendMessageInput())
	message, _ := requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, "the send checkpoint of an earlier attempt is unreadable; nothing is sent in this attempt", message)
	require.Zero(t, fake.RequestCount(graphtest.EndpointFindMessagesByMarker)+fake.RequestCount(graphtest.EndpointCreateDraft))
}

func TestSendMessageDoesNotSendWhenDexRejectsTheCheckpoint(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	ctx := newOutlookDexContext("checkpoint-rejected")
	ctx.rejectsHeartbeat = true
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), outlookConnection, validSendMessageInput())
	message, _ := requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, "Dex did not record the send checkpoint; nothing was sent", message)
	require.Zero(t, fake.RequestCount(graphtest.EndpointSendDraft))
}

func TestSendMessageRejectsInvalidInputWithoutContactingGraph(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	for _, test := range []struct {
		name    string
		change  func(*outlookmail.SendMessageInput)
		message string
	}{
		{"no recipient", func(input *outlookmail.SendMessageInput) { input.To = nil }, "to needs at least one recipient"},
		{"display name", func(input *outlookmail.SendMessageInput) { input.Cc = []string{"Jane <jane@acme.example.com>"} },
			"cc must contain bare addresses such as jane@acme.example.com"},
		{"two-line subject", func(input *outlookmail.SendMessageInput) { input.Subject = "Refund\r\nBcc: x@example.com" },
			"subject must be one line without control characters"},
		{"long subject", func(input *outlookmail.SendMessageInput) { input.Subject = strings.Repeat("s", 256) }, "subject can be at most 255 characters"},
		{"blank text", func(input *outlookmail.SendMessageInput) { input.Text = " " }, "text is required"},
		{"too many recipients", func(input *outlookmail.SendMessageInput) {
			input.Bcc = make([]string, 499)
			for index := range input.Bcc {
				input.Bcc[index] = "audit@contoso.example"
			}
		}, "to, cc, and bcc can hold at most 500 recipients together"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := validSendMessageInput()
			test.change(&input)
			result, err := sdkgo.RunMutation(newOutlookDexContext("invalid-"+test.name), client.SendMessage(), outlookConnection, input)
			require.NoError(t, err)
			require.Equal(t, outlookmail.SendMessageBranchDefect, result.Branch)
			require.Equal(t, test.message, result.Failure.Message)
		})
	}
	require.Zero(t, fake.RequestCount(graphtest.EndpointFindMessagesByMarker))
}
