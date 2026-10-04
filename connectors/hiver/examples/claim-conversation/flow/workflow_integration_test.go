//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package claimconversation

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAPIKey     = "hiverIntegrationKey0123456789"
	integrationInboxEmail = "support@acme.example.com"
	integrationAssignee   = "phoebe@acme.example.com"
	integrationInboxID    = "105902"
	integrationTagName    = "Claimed"

	// fastRequestInterval keeps most scenarios quick; the fake still enforces its own spacing.
	fastRequestInterval = 40 * time.Millisecond
	// hiverRequestInterval is Hiver's documented one request per second.
	hiverRequestInterval  = time.Second
	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second
)

func integrationClaimInput() Input {
	return Input{
		InboxEmail: integrationInboxEmail, AssigneeEmail: integrationAssignee, ClaimTagName: integrationTagName,
		Note: "Taking this one; refund check first.", ReplyDraft: "Hi Jane, we are looking into the double charge now.",
	}
}

// TestClaimAssignsTagsNotesAndDraftsAtHiversRequestRateWithRealDex runs at Hiver's documented one-request-per-second limit.
func TestClaimAssignsTagsNotesAndDraftsAtHiversRequestRateWithRealDex(t *testing.T) {
	provider := newFakeHiver(t, hiverRequestInterval)
	for index := range 100 {
		provider.seedInbox(strconv.Itoa(200+index), "team"+strconv.Itoa(index)+"@acme.example.com")
	}
	provider.seedStandardInbox()
	for index := range conversationPageSize {
		status, assignee := "closed", ""
		if index%2 == 0 {
			status, assignee = "open", "456342"
		}
		provider.seedConversation(integrationInboxID, status, assignee, nil)
	}
	target := provider.seedConversation(integrationInboxID, "open", "", []string{"784270"})
	later := provider.seedConversation(integrationInboxID, "open", "", nil)
	harness := newClaimHarness(t, provider, hiverRequestInterval, defaultRequestTimeout)

	outcome := harness.runClaim(t, "claim", integrationClaimInput())
	require.Equal(t, ClaimActionClaimed, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.Equal(t, integrationInboxID, outcome.InboxID)
	require.Equal(t, 2, outcome.InboxPagesRead)
	require.Equal(t, 2, outcome.ConversationPagesRead)
	require.Equal(t, target, outcome.Conversation.ID)
	require.Equal(t, "456342", outcome.Conversation.Assignee.ID)
	require.ElementsMatch(t, []string{"784270", "784268"}, outcome.Conversation.TagIDs)
	require.Zero(t, provider.rateLimitViolationCount(), "the connector kept Hiver's one request per second")

	conversation := provider.conversation(target)
	require.Equal(t, "456342", conversation.assigneeID)
	require.Len(t, conversation.notes, 1, "exactly one internal note")
	require.Equal(t, outcome.NoteID, conversation.notes[0].id)
	require.Equal(t, BuildClaimNote(mustClaimRequest(t, integrationClaimInput())), conversation.notes[0].content)
	require.Len(t, conversation.drafts, 1, "exactly one shared draft")
	require.Equal(t, outcome.SharedDraftID, conversation.drafts[0].id)
	require.Equal(t, conversation.messageIDs[len(conversation.messageIDs)-1], conversation.drafts[0].replyToHiverMessageID)
	require.Equal(t, integrationClaimInput().ReplyDraft, conversation.drafts[0].body)
	require.Equal(t, "", provider.conversation(later).assigneeID, "only one conversation is claimed")
	require.Equal(t, 1, provider.count("update"))
}

func TestNothingToClaimCompletesWithoutWritesWithRealDex(t *testing.T) {
	provider := newFakeHiver(t, fastRequestInterval)
	provider.seedStandardInbox()
	provider.seedConversation(integrationInboxID, "pending", "", nil)
	provider.seedConversation(integrationInboxID, "open", "456342", nil)
	harness := newClaimHarness(t, provider, fastRequestInterval, defaultRequestTimeout)

	outcome := harness.runClaim(t, "nothing", integrationClaimInput())
	require.Equal(t, ClaimActionNothingToClaim, outcome.Action)
	require.Equal(t, 1, outcome.ConversationPagesRead)
	require.Zero(t, provider.writeCount())
}

func TestConversationClaimedBetweenListAndReadIsLeftAloneWithRealDex(t *testing.T) {
	provider := newFakeHiver(t, fastRequestInterval)
	provider.seedStandardInbox()
	raced := provider.seedConversation(integrationInboxID, "open", "", nil)
	provider.claimsOnRead = raced
	harness := newClaimHarness(t, provider, fastRequestInterval, defaultRequestTimeout)

	outcome := harness.runClaim(t, "raced", integrationClaimInput())
	require.Equal(t, ClaimActionClaimedElsewhere, outcome.Action)
	require.Equal(t, raced, outcome.Conversation.ID)
	require.Equal(t, "456343", outcome.Conversation.Assignee.ID, "the read shows the other agent's claim")
	require.Zero(t, provider.writeCount())
}

// TestSlowUpdateIsSafeToRepeatWithRealDex lets async Dex dispatch the update again; both attempts send the same absolute change.
func TestSlowUpdateIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakeHiver(t, fastRequestInterval)
	provider.seedStandardInbox()
	target := provider.seedConversation(integrationInboxID, "open", "", nil)
	provider.delaysFirstUpdate = true
	harness := newClaimHarness(t, provider, fastRequestInterval, slowRequestTimeout)

	outcome := harness.runClaim(t, "slow-update", integrationClaimInput())
	require.Equal(t, ClaimActionClaimed, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("update"), 2, "Dex dispatched the update again past its local phase")
	conversation := provider.conversation(target)
	require.Equal(t, "456342", conversation.assigneeID)
	require.Equal(t, []string{"784268"}, conversation.tagIDs, "the repeated change did not duplicate the tag")
	require.Len(t, conversation.notes, 1, "exactly one internal note")
	require.Len(t, conversation.drafts, 1, "exactly one shared draft")
	t.Logf("slow update: updates=%d reads=%d", provider.count("update"), provider.count("read"))
}

// TestSlowNoteIsSentOnceWithRealDex proves sync durability never dispatches a second note while the first is in flight.
func TestSlowNoteIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeHiver(t, fastRequestInterval)
	provider.seedStandardInbox()
	target := provider.seedConversation(integrationInboxID, "open", "", nil)
	provider.delaysFirstNote = true
	harness := newClaimHarness(t, provider, fastRequestInterval, slowRequestTimeout)

	startedAt := time.Now()
	outcome := harness.runClaim(t, "slow-note", integrationClaimInput())
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, ClaimActionClaimed, outcome.Action)
	require.False(t, outcome.NeedsReview)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("note"), "no second dispatch while the first was in flight")
	require.Len(t, provider.conversation(target).notes, 1)
}

func TestLostNoteResponseSelectsUncertainAndIsNeverResentWithRealDex(t *testing.T) {
	provider := newFakeHiver(t, fastRequestInterval)
	provider.seedStandardInbox()
	target := provider.seedConversation(integrationInboxID, "open", "", nil)
	provider.losesFirstNoteResponse = true
	harness := newClaimHarness(t, provider, fastRequestInterval, defaultRequestTimeout)

	outcome := harness.runClaim(t, "lost-note", integrationClaimInput())
	require.Equal(t, ClaimActionClaimed, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "addNote", outcome.ReviewReason)
	require.Equal(t, "Hiver request failed before a response arrived", outcome.ReviewDetail)
	require.Empty(t, outcome.NoteID)
	require.Equal(t, 1, provider.count("note"), "an unconfirmed note is never resent")
	require.Len(t, provider.conversation(target).notes, 1, "Hiver did add the note, which a person must now find")
	require.Zero(t, provider.count("draft"), "the Flow stops for review before drafting")
}

func TestRateLimitedNoteWaitsAndAddsOneNoteWithRealDex(t *testing.T) {
	provider := newFakeHiver(t, fastRequestInterval)
	provider.seedStandardInbox()
	target := provider.seedConversation(integrationInboxID, "open", "", nil)
	provider.rateLimitsFirstNote = true
	harness := newClaimHarness(t, provider, fastRequestInterval, defaultRequestTimeout)

	outcome := harness.runClaim(t, "rate-limited", integrationClaimInput())
	require.Equal(t, ClaimActionClaimed, outcome.Action, "a 429 clears the dispatch marker, so the retry may send")
	require.False(t, outcome.NeedsReview)
	times := provider.requestTimes("note")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for Retry-After")
	require.Len(t, provider.conversation(target).notes, 1)
}

// TestLostWorkerDuringDraftSelectsUncertainWithoutResendingWithRealDex replaces the Worker while Hiver holds the draft.
func TestLostWorkerDuringDraftSelectsUncertainWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeHiver(t, fastRequestInterval)
	provider.seedStandardInbox()
	target := provider.seedConversation(integrationInboxID, "open", "", nil)
	provider.holdsFirstDraft = make(chan struct{})
	harness := newClaimHarness(t, provider, fastRequestInterval, slowRequestTimeout)
	flowID := harness.startClaim(t, "lost-worker", integrationClaimInput())
	require.Eventually(t, func() bool { return provider.count("draft") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Hiver")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(provider.holdsFirstDraft)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome ClaimOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, ClaimActionClaimed, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "createSharedDraft", outcome.ReviewReason)
	require.Equal(t, "an earlier attempt of this Step may have sent the request, so it is not sent again", outcome.ReviewDetail,
		"the new Worker's attempt found the dispatch marker the lost attempt recorded")
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("draft"), "the attempt on the new Worker did not resend the draft")
	require.Len(t, provider.conversation(target).drafts, 1)
}

func TestUnknownAssigneeFailsTheFlowBeforeAnyChangeWithRealDex(t *testing.T) {
	provider := newFakeHiver(t, fastRequestInterval)
	provider.seedStandardInbox()
	target := provider.seedConversation(integrationInboxID, "open", "", nil)
	harness := newClaimHarness(t, provider, fastRequestInterval, defaultRequestTimeout)
	input := integrationClaimInput()
	input.AssigneeEmail = "ben@meridian.example.com"
	flowID := harness.startClaim(t, "unknown-assignee", input)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, integrationAPIKey)
	require.Zero(t, provider.writeCount(), "nothing was changed")
	require.Empty(t, provider.conversation(target).assigneeID)
	t.Logf("unknown assignee failure: %s", result.ErrorMessage)
}

func TestUnknownInboxAddressFailsTheFlowWithRealDex(t *testing.T) {
	provider := newFakeHiver(t, fastRequestInterval)
	provider.seedStandardInbox()
	harness := newClaimHarness(t, provider, fastRequestInterval, defaultRequestTimeout)
	input := integrationClaimInput()
	input.InboxEmail = "sales@acme.example.com"
	flowID := harness.startClaim(t, "unknown-inbox", input)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "no Hiver shared inbox among the pages read has the address sales@acme.example.com")
	require.Equal(t, 1, provider.count("inboxes"))
	require.Zero(t, provider.count("conversations"))
}

func TestInvalidClaimFailsBeforeCallingHiverWithRealDex(t *testing.T) {
	provider := newFakeHiver(t, fastRequestInterval)
	harness := newClaimHarness(t, provider, fastRequestInterval, defaultRequestTimeout)
	input := integrationClaimInput()
	input.Note = " "
	flowID := harness.startClaim(t, "invalid", input)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.totalRequests())
}

func mustClaimRequest(t *testing.T, input Input) ClaimRequest {
	t.Helper()
	request, err := BuildClaimRequest(input)
	require.NoError(t, err)
	return request
}
