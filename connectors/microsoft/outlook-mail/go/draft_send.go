// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// IdempotencyMarkerPropertyID is the Outlook named extended property that carries a draft's marker.
	// Its property set GUID belongs to this connector; search a mailbox with it to find a Step's message.
	IdempotencyMarkerPropertyID = "String {194d4363-ceb1-4554-8c26-1077f4961682} Name DexStepIdempotencyKey"
	// IdempotencyMarkerPrefix begins every marker; the rest is the Step's idempotency key.
	IdempotencyMarkerPrefix = "dex-"

	// sendConfirmationDelay gives Exchange time to move a sent draft to Sent Items before the next attempt reads it.
	sendConfirmationDelay = 3 * time.Second
	// maximumMarkerMatches bounds the drafts one marker lookup reads.
	maximumMarkerMatches = 10
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// draftProperties is the $select list for a draft or its Sent Items copy.
var draftProperties = []string{
	"id", "isDraft", "subject", "toRecipients", "ccRecipients", "bccRecipients", "internetMessageId",
	"conversationId", "parentFolderId", "createdDateTime", "sentDateTime",
}

// SentMessage identifies a message sendMessage or replyToMessage sent, or the draft an uncertain send left.
type SentMessage struct {
	// MessageID is the draft's immutable Graph ID. After sending it names the Sent Items copy, so getMessage
	// can read it; an uncertain Result names the draft to inspect.
	MessageID string `json:"messageId,omitempty"`
	// InternetMessageID is the Message-ID header Exchange assigned, when Graph returned it.
	InternetMessageID string `json:"internetMessageId,omitempty"`
	// ConversationID is the thread of the message.
	ConversationID string `json:"conversationId,omitempty"`
	// Subject is the subject Exchange stored, such as RE: with the original subject for a reply.
	Subject string `json:"subject,omitempty"`
	// Recipients lists the To, Cc, and Bcc addresses of the draft, in that order.
	Recipients []string `json:"recipients,omitempty"`
	// IdempotencyMarker is the draft's IdempotencyMarkerPropertyID value, IdempotencyMarkerPrefix and the
	// Step's idempotency key.
	IdempotencyMarker string `json:"idempotencyMarker"`
	// WasAlreadySent reports that an earlier attempt of this Step execution sent the draft and this attempt
	// confirmed it from the Sent Items copy instead of sending again.
	WasAlreadySent bool `json:"wasAlreadySent,omitempty"`
}

// draftSendPhase is how far a Step execution has taken its draft.
type draftSendPhase string

const (
	// draftSendPhaseDraftReady means the draft exists and this Step execution has not dispatched its send.
	draftSendPhaseDraftReady draftSendPhase = "draftReady"
	// draftSendPhaseDispatched means a send request left the Worker; its outcome is known only after a read.
	draftSendPhaseDispatched draftSendPhase = "sendDispatched"
)

// draftSendCheckpoint is the Dex heartbeat checkpoint a send or reply records before each step that matters.
type draftSendCheckpoint struct {
	Phase          draftSendPhase `json:"outlookMailSendPhase"`
	DraftMessageID string         `json:"outlookMailDraftMessageId"`
	// Confirmations is written only by releases before v0.24.1; it is read and ignored.
	Confirmations int `json:"outlookMailSendConfirmations,omitempty"`
	// SendCount counts possibly effective sends: a dispatched checkpoint includes its own send,
	// and a draftReady checkpoint only earlier ones.
	SendCount int32 `json:"outlookMailSendCount,omitempty"`
}

// priorSendCount bounds the sends this Step execution may have made; a legacy dispatch counts as one.
func (checkpoint draftSendCheckpoint) priorSendCount() int32 {
	if checkpoint.Phase == draftSendPhaseDispatched {
		return max(checkpoint.SendCount, 1)
	}
	return checkpoint.SendCount
}

// draftCreator creates one operation's draft; the marker travels in the create request only when supported.
type draftCreator interface {
	createDraft(session *graphSession, marker string) (graphMessageWire, graphExchange)
	isMarkedOnCreation() bool
}

// draftSendBranches names an operation's branches for the shared draft send mapping.
type draftSendBranches struct {
	sent             sdkgo.BranchID
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// draftSender runs one attempt of a send or reply: confirm a dispatched draft, reuse a draft, or create one.
type draftSender struct {
	session  *graphSession
	marker   string
	creator  draftCreator
	branches draftSendBranches
	// priorSendCount bounds the sends earlier attempts of this Step execution may have made.
	priorSendCount int32
	// maxSends is the send cap of this Step execution.
	maxSends int32
}

// sendDraftOnce is the entry path shared by sendMessage and replyToMessage.
func (client *Client) sendDraftOnce(call sdkgo.Call, operation string, creator draftCreator, branches draftSendBranches) sdkgo.MutationAttempt[SentMessage] {
	marker, err := buildIdempotencyMarker(call.IdempotencyKey)
	if err != nil {
		return sdkgo.NewMutationBranch(branches.defect, SentMessage{}, graphFailurePointer(sdkgo.FailureLocalDefect, operation, err.Error()), sdkgo.Receipt{})
	}
	checkpoint, hasCheckpoint, err := readDraftSendCheckpoint(call)
	if err != nil {
		return sdkgo.NewMutationRetry[SentMessage](graphFailure(sdkgo.FailureAvailability, operation,
			"the send checkpoint of an earlier attempt is unreadable; nothing is sent in this attempt"), 0)
	}
	priorSendCount := int32(0)
	if hasCheckpoint {
		priorSendCount = checkpoint.priorSendCount()
	}
	session, failed := client.openSession(call, operation)
	if failed != nil {
		return attemptBeforeDispatch(*failed, SentMessage{IdempotencyMarker: marker}, sdkgo.Receipt{}, branches, priorSendCount)
	}
	attemptSender := draftSender{session: session, marker: marker, creator: creator, branches: branches,
		priorSendCount: priorSendCount, maxSends: call.MaxSends()}
	if hasCheckpoint && checkpoint.Phase == draftSendPhaseDispatched {
		return attemptSender.confirmDispatchedSend(checkpoint)
	}
	if hasCheckpoint && checkpoint.Phase == draftSendPhaseDraftReady {
		return attemptSender.resumeCheckpointedDraft(checkpoint.DraftMessageID)
	}
	return attemptSender.findOrCreateDraftThenSend()
}

// confirmDispatchedSend reads the dispatched draft back; only its Sent Items copy proves the send.
// A draft that is still unsent is sent again while the send cap allows.
func (sender draftSender) confirmDispatchedSend(checkpoint draftSendCheckpoint) sdkgo.MutationAttempt[SentMessage] {
	pending := SentMessage{MessageID: checkpoint.DraftMessageID, IdempotencyMarker: sender.marker}
	if validateGraphID("draft message id", checkpoint.DraftMessageID) != nil {
		// The marker lookup reads the message back without the draft ID.
		return sender.findOrCreateDraftThenSend()
	}
	draft, result := sender.readDraft(checkpoint.DraftMessageID)
	receipt := sender.session.receipt(result, checkpoint.DraftMessageID)
	switch result.outcome {
	case graphSucceeded:
		if !draft.IsDraft {
			sent := sender.sentMessage(draft)
			sent.WasAlreadySent = true
			return sdkgo.NewMutationBranch(sender.branches.sent, sent, nil, receipt)
		}
		return sender.dispatchSend(draft)
	case graphNotFound:
		return sender.findOrCreateDraftThenSend()
	case graphRetry, graphAmbiguous, graphInvalid:
		return sdkgo.NewMutationRetry[SentMessage](result.failure, result.retryAfter)
	default:
		failure := result.failure
		failure.Message += "; the dispatched draft could not be read back, so the message may have been sent; it is not sent again"
		return sdkgo.NewMutationUncertain(pending, failure, receipt)
	}
}

// resumeCheckpointedDraft continues with the draft an earlier attempt created, or finds it again if it is gone.
func (sender draftSender) resumeCheckpointedDraft(draftMessageID string) sdkgo.MutationAttempt[SentMessage] {
	if validateGraphID("draft message id", draftMessageID) != nil {
		return sender.findOrCreateDraftThenSend()
	}
	draft, result := sender.readDraft(draftMessageID)
	switch {
	case result.outcome == graphNotFound:
		return sender.findOrCreateDraftThenSend()
	case result.outcome != graphSucceeded:
		return attemptBeforeDispatch(result, SentMessage{MessageID: draftMessageID, IdempotencyMarker: sender.marker},
			sender.session.receipt(result, draftMessageID), sender.branches, sender.priorSendCount)
	case !draft.IsDraft:
		// Only this Step execution sends its draft, so a sent copy without a dispatch checkpoint was sent by it.
		sent := sender.sentMessage(draft)
		sent.WasAlreadySent = true
		return sdkgo.NewMutationBranch(sender.branches.sent, sent, nil, sender.session.receipt(result, draft.ID))
	default:
		return sender.dispatchSend(draft)
	}
}

// findOrCreateDraftThenSend looks for a draft carrying this Step's marker before creating a new one.
func (sender draftSender) findOrCreateDraftThenSend() sdkgo.MutationAttempt[SentMessage] {
	existing, isFound, result := sender.findMarkedDraft()
	if result.outcome != graphSucceeded {
		return attemptBeforeDispatch(result, SentMessage{IdempotencyMarker: sender.marker}, sender.session.receipt(result, ""), sender.branches, sender.priorSendCount)
	}
	if isFound && !existing.IsDraft {
		sent := sender.sentMessage(existing)
		sent.WasAlreadySent = true
		return sdkgo.NewMutationBranch(sender.branches.sent, sent, nil, sender.session.receipt(result, existing.ID))
	}
	if isFound {
		return sender.dispatchSend(existing)
	}
	draft, created := sender.creator.createDraft(sender.session, sender.marker)
	if created.outcome != graphSucceeded {
		return attemptBeforeDispatch(created, SentMessage{IdempotencyMarker: sender.marker}, sender.session.receipt(created, ""), sender.branches, sender.priorSendCount)
	}
	// A checkpoint the Worker could not record leaves the draft to the marker lookup.
	_ = sender.session.call.Context.RecordHeartbeat(draftSendCheckpoint{Phase: draftSendPhaseDraftReady, DraftMessageID: draft.ID, SendCount: sender.priorSendCount})
	if !sender.creator.isMarkedOnCreation() {
		marked := sender.session.exchange(graphRequest{
			method: http.MethodPatch, path: "/messages/" + url.PathEscape(draft.ID),
			payload: map[string]any{"singleValueExtendedProperties": []extendedPropertyWire{{ID: IdempotencyMarkerPropertyID, Value: sender.marker}}},
		})
		if marked.outcome == graphNotFound {
			// The draft vanished after creation; the next attempt finds the checkpointed draft gone and drafts again.
			return sdkgo.NewMutationRetry[SentMessage](marked.failure, 0)
		}
		if marked.outcome != graphSucceeded {
			return attemptBeforeDispatch(marked, sender.sentMessage(draft), sender.session.receipt(marked, draft.ID), sender.branches, sender.priorSendCount)
		}
	}
	return sender.dispatchSend(draft)
}

// dispatchSend records the dispatch checkpoint, then sends the draft while the send cap allows.
// Only an answer proving Graph did not process the send releases the checkpoint.
func (sender draftSender) dispatchSend(draft graphMessageWire) sdkgo.MutationAttempt[SentMessage] {
	operation := sender.session.operation
	message := sender.sentMessage(draft)
	if sender.priorSendCount >= sender.maxSends {
		return sdkgo.NewMutationUncertain(message, graphFailure(sdkgo.FailureTransport, operation,
			fmt.Sprintf("an earlier send of the draft was not confirmed and the send cap of %d is reached, so the message may have been sent; it is not sent again", sender.maxSends)),
			sender.session.receipt(graphExchange{}, draft.ID))
	}
	checkpoint := draftSendCheckpoint{Phase: draftSendPhaseDispatched, DraftMessageID: draft.ID, SendCount: sender.priorSendCount + 1}
	if err := sender.session.call.Context.RecordHeartbeat(checkpoint); err != nil {
		return sdkgo.NewMutationRetry[SentMessage](graphFailure(sdkgo.FailureAvailability, operation,
			"Dex did not record the send checkpoint; nothing was sent"), 0)
	}
	result := sender.session.exchange(graphRequest{method: http.MethodPost, path: "/messages/" + url.PathEscape(draft.ID) + "/send"})
	receipt := sender.session.receipt(result, draft.ID)
	switch {
	case result.outcome == graphSucceeded || result.outcome == graphInvalid:
		// Any 2xx means Graph accepted the send; the send action returns no body to validate.
		return sdkgo.NewMutationBranch(sender.branches.sent, message, nil, receipt)
	case isUnprocessedSendAnswer(result):
		sender.releaseDispatch(draft.ID)
		return sdkgo.NewMutationRetry[SentMessage](result.failure, result.retryAfter)
	case isDefiniteSendRefusal(result) && sender.priorSendCount == 0:
		sender.releaseDispatch(draft.ID)
		failure := result.failure
		failure.Message += "; nothing was sent and the draft stays in Drafts"
		return sdkgo.NewMutationBranch(sender.branches.providerRejected, message, &failure, receipt)
	case isDefiniteSendRefusal(result):
		// An earlier send may have delivered the draft, so the next attempt reads it back.
		failure := result.failure
		failure.Message += "; an earlier send may have delivered the draft, so a later attempt reads it back"
		return sdkgo.NewMutationRetry[SentMessage](failure, sendConfirmationDelay)
	default:
		failure := result.failure
		failure.Message += "; the send was dispatched, so a later attempt reads the draft back before sending again"
		return sdkgo.NewMutationRetry[SentMessage](failure, sendConfirmationDelay)
	}
}

// releaseDispatch returns the checkpoint to draftReady after Graph provably did not process the send.
func (sender draftSender) releaseDispatch(draftMessageID string) {
	// A failed release keeps the dispatch checkpoint, so the next attempt reads the draft instead of sending.
	_ = sender.session.call.Context.RecordHeartbeat(draftSendCheckpoint{Phase: draftSendPhaseDraftReady, DraftMessageID: draftMessageID, SendCount: sender.priorSendCount})
}

// isUnprocessedSendAnswer reports a 429, a 503, or a connection that never reached Graph.
func isUnprocessedSendAnswer(result graphExchange) bool {
	if result.statusCode == 0 {
		return result.outcome == graphRetry
	}
	return result.statusCode == http.StatusTooManyRequests || result.statusCode == http.StatusServiceUnavailable
}

// isDefiniteSendRefusal reports a definite refusal status, or a local request defect before sending.
func isDefiniteSendRefusal(result graphExchange) bool {
	if result.statusCode == 0 {
		return result.outcome == graphDefect
	}
	return isDefiniteRefusalStatus(result.statusCode)
}

// findMarkedDraft returns a sent copy before any draft, then the earliest-created draft.
func (sender draftSender) findMarkedDraft() (graphMessageWire, bool, graphExchange) {
	filter := "singleValueExtendedProperties/Any(ep: ep/id eq " + quoteODataString(IdempotencyMarkerPropertyID) +
		" and ep/value eq " + quoteODataString(sender.marker) + ")"
	result := sender.session.exchange(graphRequest{
		method: http.MethodGet, path: "/messages",
		query: graphQuery{
			{name: "$filter", value: filter}, {name: "$select", value: strings.Join(draftProperties, ",")},
			{name: "$top", value: "10"},
		},
	})
	if result.outcome != graphSucceeded {
		return graphMessageWire{}, false, result
	}
	var envelope struct {
		Value *[]json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(result.body, &envelope); err != nil || envelope.Value == nil {
		return graphMessageWire{}, false, invalidDraftAnswer(result, sender.session.operation, "the draft lookup has no value list")
	}
	var matches []graphMessageWire
	for _, item := range (*envelope.Value)[:min(len(*envelope.Value), maximumMarkerMatches)] {
		match, err := decodeGraphMessage(item)
		if err != nil {
			return graphMessageWire{}, false, invalidDraftAnswer(result, sender.session.operation, "the draft lookup returned "+err.Error())
		}
		matches = append(matches, match)
	}
	if len(matches) == 0 {
		return graphMessageWire{}, false, result
	}
	sort.SliceStable(matches, func(left, right int) bool {
		if matches[left].IsDraft != matches[right].IsDraft {
			return !matches[left].IsDraft
		}
		return createdBefore(matches[left], matches[right])
	})
	return matches[0], true, result
}

// readDraft reads one draft or its Sent Items copy by immutable ID.
func (sender draftSender) readDraft(draftMessageID string) (graphMessageWire, graphExchange) {
	result := sender.session.exchange(graphRequest{
		method: http.MethodGet, path: "/messages/" + url.PathEscape(draftMessageID),
		query: graphQuery{{name: "$select", value: strings.Join(draftProperties, ",")}},
	})
	if result.outcome != graphSucceeded {
		return graphMessageWire{}, result
	}
	draft, err := decodeGraphMessage(result.body)
	if err != nil {
		return graphMessageWire{}, invalidDraftAnswer(result, sender.session.operation, "the draft read returned "+err.Error())
	}
	return draft, result
}

// attemptBeforeDispatch maps a failure before this attempt dispatched a send. A terminal answer
// selects uncertain when an earlier attempt may have sent the draft.
func attemptBeforeDispatch(result graphExchange, value SentMessage, receipt sdkgo.Receipt, branches draftSendBranches, priorSendCount int32) sdkgo.MutationAttempt[SentMessage] {
	switch result.outcome {
	case graphRetry, graphAmbiguous, graphInvalid:
		return sdkgo.NewMutationRetry[SentMessage](result.failure, result.retryAfter)
	}
	if priorSendCount > 0 {
		failure := result.failure
		failure.Message += "; an earlier send may have delivered the message, and it cannot be confirmed or sent again"
		return sdkgo.NewMutationUncertain(value, failure, receipt)
	}
	switch result.outcome {
	case graphNotFound:
		if branches.notFound != "" {
			return sdkgo.NewMutationBranch(branches.notFound, value, &result.failure, receipt)
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, value, &result.failure, receipt)
	case graphDefect:
		return sdkgo.NewMutationBranch(branches.defect, value, &result.failure, receipt)
	default:
		failure := result.failure
		failure.Message += "; nothing was sent"
		return sdkgo.NewMutationBranch(branches.providerRejected, value, &failure, receipt)
	}
}

func (sender draftSender) sentMessage(draft graphMessageWire) SentMessage {
	return SentMessage{
		MessageID: draft.ID, InternetMessageID: draft.InternetMessageID, ConversationID: draft.ConversationID,
		Subject: draft.Subject, Recipients: draft.recipientAddresses(), IdempotencyMarker: sender.marker,
	}
}

// extendedPropertyWire is one Graph singleValueLegacyExtendedProperty.
type extendedPropertyWire struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// buildIdempotencyMarker derives the draft marker from the Step's idempotency key, which every attempt shares.
func buildIdempotencyMarker(key sdkgo.IdempotencyKey) (string, error) {
	if !idempotencyKeyPattern.MatchString(string(key)) {
		return "", errors.New("the Step has no usable idempotency key for the draft marker")
	}
	return IdempotencyMarkerPrefix + string(key), nil
}

// readDraftSendCheckpoint reads the last attempt's checkpoint; a value that is not a send checkpoint is an error.
func readDraftSendCheckpoint(call sdkgo.Call) (draftSendCheckpoint, bool, error) {
	var checkpoint draftSendCheckpoint
	isFound, err := call.Context.GetLastHeartbeatValue(&checkpoint)
	switch {
	case err != nil:
		return draftSendCheckpoint{}, false, err
	case !isFound:
		return draftSendCheckpoint{}, false, nil
	case checkpoint.Phase != draftSendPhaseDraftReady && checkpoint.Phase != draftSendPhaseDispatched || checkpoint.SendCount < 0:
		return draftSendCheckpoint{}, false, errors.New("the heartbeat checkpoint is not a send checkpoint")
	}
	return checkpoint, true, nil
}

func invalidDraftAnswer(result graphExchange, operation string, message string) graphExchange {
	result.outcome = graphInvalid
	result.failure = graphFailure(sdkgo.FailureProtocol, operation, "Microsoft Graph returned an invalid answer: "+message)
	return result
}

func createdBefore(left graphMessageWire, right graphMessageWire) bool {
	switch {
	case left.CreatedDateTime == nil:
		return false
	case right.CreatedDateTime == nil:
		return true
	case !left.CreatedDateTime.Equal(*right.CreatedDateTime):
		return left.CreatedDateTime.Before(*right.CreatedDateTime)
	default:
		return left.ID < right.ID
	}
}
