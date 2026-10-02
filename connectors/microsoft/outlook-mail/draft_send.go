// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail

import (
	"encoding/json"
	"errors"
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
	// maximumSendConfirmations bounds how many attempts read a dispatched draft before reporting uncertain.
	maximumSendConfirmations = 3
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
	Confirmations  int            `json:"outlookMailSendConfirmations,omitempty"`
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
}

// sendDraftOnce is the entry path shared by sendMessage and replyToMessage.
func (client *Client) sendDraftOnce(call sdkgo.Call, operation string, creator draftCreator, branches draftSendBranches) sdkgo.MutationAttempt[SentMessage] {
	marker, err := buildIdempotencyMarker(call.IdempotencyKey)
	if err != nil {
		return sdkgo.NewMutationBranch(branches.defect, SentMessage{}, graphFailurePointer(sdkgo.FailureLocalDefect, operation, err.Error()), sdkgo.Receipt{})
	}
	checkpoint, hasCheckpoint, err := readDraftSendCheckpoint(call)
	if err != nil {
		return sdkgo.NewMutationUncertain(SentMessage{IdempotencyMarker: marker}, graphFailure(sdkgo.FailureLocalDefect, operation,
			"the send checkpoint of an earlier attempt is unreadable, so the message may have been sent; it is not sent again"), sdkgo.Receipt{})
	}
	session, failed := client.openSession(call, operation)
	if failed != nil {
		return attemptBeforeDispatch(*failed, SentMessage{IdempotencyMarker: marker}, sdkgo.Receipt{}, branches)
	}
	attemptSender := draftSender{session: session, marker: marker, creator: creator, branches: branches}
	if hasCheckpoint && checkpoint.Phase == draftSendPhaseDispatched {
		return attemptSender.confirmDispatchedSend(checkpoint)
	}
	if hasCheckpoint && checkpoint.Phase == draftSendPhaseDraftReady {
		return attemptSender.resumeCheckpointedDraft(checkpoint.DraftMessageID)
	}
	return attemptSender.findOrCreateDraftThenSend()
}

// confirmDispatchedSend reads the dispatched draft; only its Sent Items copy proves the send.
func (sender draftSender) confirmDispatchedSend(checkpoint draftSendCheckpoint) sdkgo.MutationAttempt[SentMessage] {
	operation := sender.session.operation
	pending := SentMessage{MessageID: checkpoint.DraftMessageID, IdempotencyMarker: sender.marker}
	if validateGraphID("draft message id", checkpoint.DraftMessageID) != nil {
		return sdkgo.NewMutationUncertain(pending, graphFailure(sdkgo.FailureLocalDefect, operation,
			"an earlier attempt dispatched the send without naming its draft, so the message may have been sent; it is not sent again"), sdkgo.Receipt{})
	}
	draft, result := sender.readDraft(checkpoint.DraftMessageID)
	receipt := sender.session.receipt(result, checkpoint.DraftMessageID)
	if result.outcome == graphSucceeded && !draft.IsDraft {
		sent := sender.sentMessage(draft)
		sent.WasAlreadySent = true
		return sdkgo.NewMutationBranch(sender.branches.sent, sent, nil, receipt)
	}
	if result.outcome == graphSucceeded {
		pending = sender.sentMessage(draft)
	}
	if checkpoint.Confirmations+1 < maximumSendConfirmations {
		checkpoint.Confirmations++
		if sender.session.call.Context.RecordHeartbeat(checkpoint) == nil {
			return sdkgo.NewMutationRetry[SentMessage](graphFailure(sdkgo.FailureAvailability, operation,
				"the send was dispatched but its answer was lost; the draft has not appeared as sent yet, so a later attempt reads it again"),
				sendConfirmationDelay)
		}
	}
	return sdkgo.NewMutationUncertain(pending, graphFailure(sdkgo.FailureTransport, operation,
		"the send was dispatched but its answer was lost and the draft has not appeared as sent, so the message may have been sent; it is not sent again"),
		receipt)
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
			sender.session.receipt(result, draftMessageID), sender.branches)
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
		return attemptBeforeDispatch(result, SentMessage{IdempotencyMarker: sender.marker}, sender.session.receipt(result, ""), sender.branches)
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
		return attemptBeforeDispatch(created, SentMessage{IdempotencyMarker: sender.marker}, sender.session.receipt(created, ""), sender.branches)
	}
	// A checkpoint the Worker could not record leaves the draft to the marker lookup.
	_ = sender.session.call.Context.RecordHeartbeat(draftSendCheckpoint{Phase: draftSendPhaseDraftReady, DraftMessageID: draft.ID})
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
			return attemptBeforeDispatch(marked, sender.sentMessage(draft), sender.session.receipt(marked, draft.ID), sender.branches)
		}
	}
	return sender.dispatchSend(draft)
}

// dispatchSend records the dispatch checkpoint, then sends the draft; only a provable refusal clears it.
func (sender draftSender) dispatchSend(draft graphMessageWire) sdkgo.MutationAttempt[SentMessage] {
	operation := sender.session.operation
	message := sender.sentMessage(draft)
	if err := sender.session.call.Context.RecordHeartbeat(draftSendCheckpoint{Phase: draftSendPhaseDispatched, DraftMessageID: draft.ID}); err != nil {
		return sdkgo.NewMutationRetry[SentMessage](graphFailure(sdkgo.FailureAvailability, operation,
			"Dex did not record the send checkpoint; nothing was sent"), 0)
	}
	result := sender.session.exchange(graphRequest{method: http.MethodPost, path: "/messages/" + url.PathEscape(draft.ID) + "/send"})
	receipt := sender.session.receipt(result, draft.ID)
	switch result.outcome {
	case graphSucceeded, graphInvalid:
		// Any 2xx means Graph accepted the send; the send action returns no body to validate.
		return sdkgo.NewMutationBranch(sender.branches.sent, message, nil, receipt)
	case graphRetry:
		sender.releaseDispatch(draft.ID)
		return sdkgo.NewMutationRetry[SentMessage](result.failure, result.retryAfter)
	case graphRejected, graphNotFound, graphDefect:
		sender.releaseDispatch(draft.ID)
		failure := result.failure
		failure.Message += "; nothing was sent and the draft stays in Drafts"
		return sdkgo.NewMutationBranch(sender.branches.providerRejected, message, &failure, receipt)
	default:
		failure := result.failure
		failure.Message += "; the send was dispatched, so a later attempt confirms it from the draft instead of sending again"
		return sdkgo.NewMutationRetry[SentMessage](failure, sendConfirmationDelay)
	}
}

// releaseDispatch returns the checkpoint to draftReady after Graph provably refused the send.
func (sender draftSender) releaseDispatch(draftMessageID string) {
	// A failed release keeps the dispatch checkpoint, so the next attempt reads the draft instead of sending.
	_ = sender.session.call.Context.RecordHeartbeat(draftSendCheckpoint{Phase: draftSendPhaseDraftReady, DraftMessageID: draftMessageID})
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

// attemptBeforeDispatch maps a failure before any send was dispatched, when every retry is safe.
func attemptBeforeDispatch(result graphExchange, value SentMessage, receipt sdkgo.Receipt, branches draftSendBranches) sdkgo.MutationAttempt[SentMessage] {
	switch result.outcome {
	case graphRetry, graphAmbiguous, graphInvalid:
		return sdkgo.NewMutationRetry[SentMessage](result.failure, result.retryAfter)
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
	case checkpoint.Phase != draftSendPhaseDraftReady && checkpoint.Phase != draftSendPhaseDispatched:
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
