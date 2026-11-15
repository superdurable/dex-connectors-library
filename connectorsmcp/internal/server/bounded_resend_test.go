// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/attemptcontext"
	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/testconnectors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	limitedMailTool = "example-mail_send_message"
	unsafeMailTool  = "example-mail_send_alert"
)

var mailArguments = map[string]any{"to": "ops@example.com", "text": "Deploy finished."}

// lostAfterSend applies the send, then answers 500, so the outcome is unconfirmed.
var lostAfterSend = testconnectors.ScriptedResponse{Status: http.StatusInternalServerError, IsEffectAppliedFirst: true}

func TestALimitedWriteWhoseSendIsUnconfirmedIsResentUnderOnePinnedCap(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{isMailLinked: true})
	harness.provider.Script("sendMessage", lostAfterSend)
	result := harness.mustCallResult(limitedMailTool, mailArguments, "flow/turn/mail-1")
	require.False(t, result.IsError, result.text())
	require.JSONEq(t, `{"messageId":"m-2"}`, result.text())
	require.Equal(t, 2, harness.provider.MessageRequests(), "one resend after the unconfirmed send")
	require.Equal(t, 2, harness.provider.CreatedMessages(), "a limited write accepts the duplicate")
	require.Equal(t, []int32{testconnectors.MailMaxSends, testconnectors.MailMaxSends}, harness.mailAttempts.MaxSendsPerAttempt(),
		"every attempt of the call runs with the manifest cap")
	require.Equal(t, []time.Duration{time.Second}, harness.clock.recordedSleeps())

	replay := harness.mustCallResult(limitedMailTool, mailArguments, "flow/turn/mail-1")
	require.Equal(t, true, replay.Meta[metaKeyReplayed])
	require.Equal(t, result.text(), replay.text())
	require.Equal(t, 2, harness.provider.MessageRequests(), "a replay never sends")
}

func TestALimitedWriteAtItsSendCapIsUnknown(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{isMailLinked: true})
	harness.provider.Script("sendMessage", lostAfterSend, lostAfterSend, lostAfterSend)
	result := harness.mustCallResult(limitedMailTool, mailArguments, "flow/turn/mail-cap")
	require.Equal(t, "unknown", result.outcome(), result.text())
	require.Equal(t, string(sdkgo.UncertainBranchID), result.Meta[metaKeyBranch], "the Fence refused a fourth send")
	require.Equal(t, int(testconnectors.MailMaxSends), harness.provider.MessageRequests(), "exactly the cap reached the provider")
	require.Equal(t, []int32{3, 3, 3, 3}, harness.mailAttempts.MaxSendsPerAttempt(), "three sends and the attempt that found the cap reached")
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, harness.clock.recordedSleeps())

	again := harness.mustCallResult(limitedMailTool, mailArguments, "flow/turn/mail-cap")
	require.Equal(t, true, again.Meta[metaKeyReplayed])
	require.Equal(t, "unknown", again.outcome())
	require.Equal(t, int(testconnectors.MailMaxSends), harness.provider.MessageRequests())
}

func TestALimitedWriteWhoseAttemptLimitEndsFirstIsUnknown(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{isMailLinked: true})
	// A 429 records canResend, so the Fence keeps allowing sends; only the attempt limit stops them.
	for range 6 {
		harness.provider.Script("sendMessage", testconnectors.ScriptedResponse{Status: http.StatusTooManyRequests})
	}
	result := harness.mustCallResult(limitedMailTool, mailArguments, "flow/turn/mail-throttled")
	require.Equal(t, "unknown", result.outcome(), result.text())
	require.Contains(t, result.text(), "still asked to retry after 5 attempts")
	require.Equal(t, 5, harness.provider.MessageRequests(), "three inline attempts plus one per extra send")
}

func TestAWriteThatIsNotLimitedIsNeverResent(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{isMailLinked: true})
	harness.provider.Script("sendMessage", lostAfterSend)
	unsafe := harness.mustCallResult(unsafeMailTool, mailArguments, "flow/turn/alert-1")
	require.Equal(t, "unknown", unsafe.outcome(), unsafe.text())
	require.Equal(t, 1, harness.provider.MessageRequests())
	require.Equal(t, []int32{1}, harness.mailAttempts.MaxSendsPerAttempt(), "an unsafe write runs one attempt without a send cap option")

	// postNote's metadata predates repeatOnUnknown, so it keeps one attempt too.
	harness.provider.Script("postNote", testconnectors.ScriptedResponse{Status: http.StatusServiceUnavailable})
	note := harness.mustCallResult("example-notes_post_note", map[string]any{"text": "hello"}, "flow/turn/post-1")
	require.Equal(t, "unknown", note.outcome(), note.text())
	require.Equal(t, 1, harness.provider.NoteRequests())
	require.Empty(t, harness.clock.recordedSleeps(), "neither write is retried inline")
}

func TestALimitedWriteDefiniteRejectionIsNotResent(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{isMailLinked: true})
	beforeSend := harness.mustCallResult(limitedMailTool, map[string]any{"to": " ", "text": "x"}, "flow/turn/mail-blank")
	require.Equal(t, "rejected", beforeSend.outcome(), beforeSend.text())
	require.Equal(t, "invalidRecipient", beforeSend.Meta[metaKeyBranch])
	require.Equal(t, 0, harness.provider.MessageRequests())

	harness.provider.Script("sendMessage", testconnectors.ScriptedResponse{Status: http.StatusBadRequest})
	afterSend := harness.mustCallResult(limitedMailTool, mailArguments, "flow/turn/mail-refused")
	require.Equal(t, "unknown", afterSend.outcome(), "the Fence claimed a send, so even a refusal cannot prove nothing reached the provider")
	require.Equal(t, "recipientRefused", afterSend.Meta[metaKeyBranch])
	require.Equal(t, 1, harness.provider.MessageRequests(), "a definite rejection is never resent")
	require.Empty(t, harness.clock.recordedSleeps())
}

func TestALimitedWriteWhoseBudgetIsSpentSettlesInsteadOfAskingForARepeat(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{isMailLinked: true, callTimeBudget: 30500 * time.Millisecond})
	harness.provider.Script("sendMessage", lostAfterSend)
	result := harness.mustCallResult(limitedMailTool, mailArguments, "flow/turn/mail-budget")
	require.Equal(t, "unknown", result.outcome(), result.text())
	require.Equal(t, 1, harness.provider.MessageRequests())
	again := harness.mustCallResult(limitedMailTool, mailArguments, "flow/turn/mail-budget")
	require.Equal(t, true, again.Meta[metaKeyReplayed], "the key settled, so a repeat replays instead of resending")
	require.Equal(t, 1, harness.provider.MessageRequests())
}

func TestALimitedWriteWhoseRetryDelayEndsSettlesWithoutAnotherSend(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{isMailLinked: true})
	harness.provider.Script("sendMessage", lostAfterSend)
	// Stopping the server during the delay ends the wait as a cancelled client would.
	harness.clock.setOnSleep(harness.server.BeginDrain)
	result := harness.mustCallResult(limitedMailTool, mailArguments, "flow/turn/mail-drained")
	require.Equal(t, "unknown", result.outcome(), result.text())
	require.Contains(t, result.text(), "may have taken effect: the provider asked to retry later")
	require.Len(t, harness.clock.recordedSleeps(), 1, "the call settled after its one delay")
	require.Equal(t, 1, harness.provider.MessageRequests())

	again := harness.mustCallResult(limitedMailTool, mailArguments, "flow/turn/mail-drained")
	require.Equal(t, true, again.Meta[metaKeyReplayed], "the key settled during the delay")
	require.Equal(t, result.text(), again.text())
	require.Equal(t, 1, harness.provider.MessageRequests(), "nothing more was sent")
}

func TestAReissueOfARunningLimitedKeyWaitsAndReplays(t *testing.T) {
	arrivals := make(chan struct{}, 4)
	harness := newTestHarness(t, harnessOptions{isMailLinked: true, observe: func(point ProtocolPoint, _ string) {
		if point == ProtocolPointBeforeKeyLock {
			arrivals <- struct{}{}
		}
	}})
	// The resend is held, so the key is still running when the re-issue arrives.
	harness.provider.Script("sendMessage", lostAfterSend, testconnectors.ScriptedResponse{IsHeld: true})
	first := callInBackground(harness, limitedMailTool, mailArguments, "flow/turn/mail-reissue")
	<-arrivals
	require.Equal(t, "sendMessage", <-harness.provider.HeldRequests(), "the second send of the first call is in flight")
	second := callInBackground(harness, limitedMailTool, mailArguments, "flow/turn/mail-reissue")
	<-arrivals
	harness.provider.Release()

	firstOutcome := <-first
	require.NoError(t, firstOutcome.err)
	require.Nil(t, firstOutcome.response.Error)
	var firstResult callResult
	require.NoError(t, json.Unmarshal(firstOutcome.response.Result, &firstResult))
	require.False(t, firstResult.IsError, firstResult.text())
	require.JSONEq(t, `{"messageId":"m-2"}`, firstResult.text())

	secondOutcome := <-second
	require.NoError(t, secondOutcome.err)
	require.Nil(t, secondOutcome.response.Error)
	var secondResult callResult
	require.NoError(t, json.Unmarshal(secondOutcome.response.Result, &secondResult))
	require.Equal(t, true, secondResult.Meta[metaKeyReplayed], "the re-issue waits for the key and replays")
	require.Equal(t, firstResult.text(), secondResult.text())
	require.Equal(t, 2, harness.provider.MessageRequests(), "the re-issue sent nothing")
}

func TestALimitedWriteIsListedAsANonIdempotentWrite(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{isMailLinked: true})
	byName := map[string]map[string]any{}
	for _, tool := range harness.listTools() {
		byName[tool["name"].(string)] = tool
	}
	limited := byName[limitedMailTool]
	require.NotNil(t, limited)
	meta := limited["_meta"].(map[string]any)
	require.Equal(t, "non_idempotent_write", meta[metaKeyEffect], "Dex conventions have no effect value for a limited write")
	require.Equal(t, "recommended", meta[metaKeyApproval], "approval follows agent.approval alone")
	require.Equal(t, false, limited["annotations"].(map[string]any)["idempotentHint"])
	unsafeMeta := byName[unsafeMailTool]["_meta"].(map[string]any)
	require.Equal(t, "non_idempotent_write", unsafeMeta[metaKeyEffect])
	_, hasApproval := unsafeMeta[metaKeyApproval]
	require.False(t, hasApproval)
}

func TestPinnedSendCapFollowsTheRepeatOnUnknownTier(t *testing.T) {
	mutation := sdkgo.AgentToolMetadata{Kind: sdkgo.AgentOperationKindMutation}
	limited := func(maxSends int32) sdkgo.AgentToolMetadata {
		metadata := mutation
		metadata.RepeatOnUnknown, metadata.MaxSends = sdkgo.RepeatOnUnknownLimited, maxSends
		return metadata
	}
	withTier := func(tier sdkgo.RepeatOnUnknown, isRetrySafe bool) sdkgo.AgentToolMetadata {
		metadata := mutation
		metadata.RepeatOnUnknown, metadata.IsRetrySafe = tier, isRetrySafe
		return metadata
	}
	scenarios := []struct {
		name     string
		metadata sdkgo.AgentToolMetadata
		sendCap  int32
	}{
		{"query", sdkgo.AgentToolMetadata{Kind: sdkgo.AgentOperationKindQuery}, 0},
		{"mutation before repeatOnUnknown", mutation, 0},
		{"retry-safe mutation before repeatOnUnknown", withTier("", true), 0},
		{"safe", withTier(sdkgo.RepeatOnUnknownSafe, true), 0},
		{"unsafe", withTier(sdkgo.RepeatOnUnknownUnsafe, false), 0},
		{"limited with the default cap", limited(3), 3},
		{"limited at the ceiling", limited(sdkgo.MaxSendsCeiling), sdkgo.MaxSendsCeiling},
		{"limited with cap 1", limited(1), 1},
		{"limited without a cap", limited(0), 1},
	}
	for _, scenario := range scenarios {
		sendCap := pinnedSendCap(scenario.metadata)
		require.Equal(t, scenario.sendCap, sendCap, scenario.name)
		require.Equal(t, sendCap >= 2, allowsBoundedResend(sendCap), scenario.name)
		require.Len(t, sendCapRunOptions(sendCap), map[bool]int{true: 1, false: 0}[sendCap != 0], scenario.name)
	}

	policy := sdkgo.DefaultConnectorStepExecuteRetryPolicy()
	require.Equal(t, int32(3), writeAttemptLimit(policy, 3, 0))
	require.Equal(t, int32(3), writeAttemptLimit(policy, 3, 1), "cap 1 never resends, so it adds no attempt")
	require.Equal(t, int32(5), writeAttemptLimit(policy, 3, 3))
	policy.MaximumAttempts = 2
	require.Equal(t, int32(2), writeAttemptLimit(policy, 3, 3), "a manifest attempt limit still holds")
}

func TestASendCapOptionOnATierThatIsNotLimitedWouldRefuseTheWrite(t *testing.T) {
	// The host must never pass WithMaxSends to these tools: the SDK refuses the
	// attempt before Invoke, which a write would answer as rejected.
	for _, metadata := range testconnectors.MailExposedAgentTools()[1:] {
		require.Zero(t, pinnedSendCap(metadata), metadata.ToolName)
	}
	operation := &callIDCapturingMutation{}
	metadata := sdkgo.AgentToolMetadata{
		Operation: operation.Definition().Operation, Kind: sdkgo.AgentOperationKindMutation, ToolName: "example-chat_post_reply",
		Description: "Post.", InputSchema: json.RawMessage(`{"type":"object"}`), Branches: operation.Definition().Branches,
	}
	tool, err := sdkgo.NewMutationAgentTool[struct{}, struct{}](operation, sdkgo.ConnectionRef{Provider: "chat", Name: "work"}, metadata)
	require.NoError(t, err)
	dexContext, err := attemptcontext.New(t.Context(), attemptcontext.Options{FlowID: localFlowID, StepExecutionID: "key", Attempt: 1})
	require.NoError(t, err)
	_, err = tool.RunAttemptWithOptions(dexContext, json.RawMessage(`{}`), sdkgo.WithMaxSends(1))
	require.True(t, errors.Is(err, sdkgo.ErrAgentToolRunOptionInvalid))
	_, err = tool.RunAttemptWithOptions(dexContext, json.RawMessage(`{}`), sendCapRunOptions(pinnedSendCap(metadata))...)
	require.NoError(t, err)
}
