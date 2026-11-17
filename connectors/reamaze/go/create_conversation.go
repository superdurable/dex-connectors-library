// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	createConversationOperation = "createConversation"

	// DispatchKeyDataAttribute is the conversation data attribute createConversation sets to the
	// Step's dispatch key, so an unconfirmed attempt can find the conversation it created.
	DispatchKeyDataAttribute = "dex_dispatch_key"
)

// CreateConversationInput is one new conversation on behalf of a customer. Zero values leave a
// field to Re:amaze's defaults and the channel's automations.
type CreateConversationInput struct {
	// Subject is the conversation subject. It is required.
	Subject string `json:"subject"`
	// Message is the customer's first message, at most MaxTextBytes. It is required and sent
	// unchanged as Re:amaze's message body.
	Message string `json:"message"`
	// Channel is the slug of the channel to create the conversation in, Re:amaze's category, such
	// as support. It is required; Settings > Channels lists each channel and its slug.
	Channel string `json:"channel"`
	// Requester is the customer the conversation is created for. It is required.
	Requester ConversationRequesterInput `json:"requester"`
	// Status is a Re:amaze status, or nil for Re:amaze's default.
	Status *ConversationStatus `json:"status,omitempty"`
	// HoldUntil is when an On Hold (5) conversation's reminder fires, an RFC 3339 instant. It
	// requires Status 5; blank sets no reminder time.
	HoldUntil string `json:"holdUntil,omitempty"`
	// Tags are set on the new conversation; channel automations may add more.
	Tags []string `json:"tags,omitempty"`
	// AssigneeEmail assigns the staff user with this email, or blank for Re:amaze's routing.
	AssigneeEmail string `json:"assigneeEmail,omitempty"`
	// ShouldSuppressNotifications asks Re:amaze to send no email or integration notification for
	// the first message, Re:amaze's message suppress_notifications.
	ShouldSuppressNotifications bool `json:"shouldSuppressNotifications,omitempty"`
}

// ConversationRequesterInput identifies the customer by email. Re:amaze uses the contact with
// that address or adds one named Name.
type ConversationRequesterInput struct {
	// Email is one bare address such as jane@example.com.
	Email string `json:"email"`
	// Name names a contact Re:amaze adds; blank sends no name.
	Name string `json:"name,omitempty"`
}

// CreateConversationOutput is the created conversation.
type CreateConversationOutput struct {
	// Conversation is the conversation Re:amaze created.
	Conversation Conversation `json:"conversation"`
	// WasAlreadyApplied reports that an earlier attempt of this Step created the conversation
	// without a confirmed outcome and this attempt found it, so nothing was sent again.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// CreateConversationOperation is the createConversation Mutation.
type CreateConversationOperation struct {
	client *Client
}

type createConversationRequestWire struct {
	Conversation createConversationWire `json:"conversation"`
}

type createConversationWire struct {
	Subject   string                        `json:"subject"`
	Category  string                        `json:"category"`
	TagList   []string                      `json:"tag_list,omitempty"`
	Status    *int                          `json:"status,omitempty"`
	HoldUntil string                        `json:"hold_until,omitempty"`
	Assignee  string                        `json:"assignee,omitempty"`
	Data      map[string]string             `json:"data"`
	Message   createConversationMessageWire `json:"message"`
	User      participantRequestWire        `json:"user"`
}

type createConversationMessageWire struct {
	Body                        string `json:"body"`
	ShouldSuppressNotifications bool   `json:"suppress_notifications,omitempty"`
}

type participantRequestWire struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

// dispatchKeyMatch is a list read-back: a confirmed conversation, or a candidate whose data the list omitted.
type dispatchKeyMatch struct {
	conversation  Conversation
	isConfirmed   bool
	candidateSlug string
}

// Definition returns the immutable connector operation definition.
func (CreateConversationOperation) Definition() sdkgo.MutationDefinition {
	return CreateConversationDefinition
}

// IdempotencyKey uses the stable connector Call ID. Re:amaze documents no idempotency key, so the
// key becomes the conversation's DispatchKeyDataAttribute, which reconciliation searches for.
func (CreateConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke fences the create request and reconciles an unconfirmed write by its conversation data key.
func (operation CreateConversationOperation) Invoke(call sdkgo.Call, input CreateConversationInput) sdkgo.MutationAttempt[CreateConversationOutput] {
	if err := validateCreateConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(CreateConversationBranchDefect, CreateConversationOutput{}, reamazeFailurePointer(sdkgo.FailureValidation, createConversationOperation, err.Error()), sdkgo.Receipt{})
	}
	fence, err := newReamazeFence(call, CreateConversationDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateConversationBranchDefect, CreateConversationOutput{}, reamazeFailurePointer(sdkgo.FailureLocalDefect, createConversationOperation, err.Error()), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	var credentials Credentials
	switch decision.Kind {
	case singledispatch.FailClosed:
		// FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
		return sdkgo.NewMutationRetry[CreateConversationOutput](reamazeFailure(sdkgo.FailureAvailability, createConversationOperation,
			"dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
	case singledispatch.ReadBackRequired, singledispatch.ReadBackThenResendAllowed:
		credentials, err = operation.client.resolveCredentials(call)
		if err != nil {
			return uncertainOrRetryCredentials[CreateConversationOutput](operation.client, call, createConversationOperation, decision.Marker.ProviderID, err)
		}
		// A saved conversation ID proves Re:amaze created it, so it is read back and never resent.
		if decision.Marker.ProviderID != "" {
			return operation.confirmDispatchCandidate(call, credentials, decision.Marker.ProviderID, dispatchKey(call)).uncertainWhenUnconfirmed(CreateConversationOutput{})
		}
		readBack := operation.reconcileEarlierDispatch(call, credentials)
		if decision.Kind == singledispatch.ReadBackRequired || !readBack.isUnconfirmed {
			return readBack.uncertainWhenUnconfirmed(CreateConversationOutput{})
		}
		// The read-back found no conversation with the key, so this attempt resends below the cap.
	default:
		credentials, err = operation.client.resolveCredentials(call)
		if err != nil {
			return fencedCredentialAttempt[CreateConversationOutput](fence, createConversationOperation, CreateConversationBranchDefect, err)
		}
	}
	permit := newReamazeWritePermit(call, fence, decision)
	send := &fencedReamazeSend{client: operation.client, call: call, credentials: credentials, operation: createConversationOperation,
		request: reamazeRequest{method: http.MethodPost, path: "/conversations", payload: buildCreateConversationRequest(input, dispatchKey(call))}}
	if err := fence.Dispatch(send.dispatch); err != nil {
		return fencedDispatchError[CreateConversationOutput](createConversationOperation, err)
	}
	result := send.result
	receipt := operation.client.receipt(call, result.response, "")
	if hasUnusableAcceptedWrite(result) {
		return operation.reconcileEarlierDispatch(call, credentials).resendOrSelectUncertain(CreateConversationOutput{}, permit)
	}
	if attempt, isTerminal := fencedWriteResult[CreateConversationOutput](permit, result, receipt, fencedWriteBranches{
		providerRejected: CreateConversationBranchProviderRejected, defect: CreateConversationBranchDefect,
	}); isTerminal {
		return attempt
	}
	conversation, err := decodeConversationBody(result.response.body, "")
	if err != nil {
		return operation.reconcileEarlierDispatch(call, credentials).resendOrSelectUncertain(CreateConversationOutput{}, permit)
	}
	_ = fence.RecordProviderID(conversation.ID)
	return sdkgo.NewMutationBranch(CreateConversationBranchCreated, CreateConversationOutput{Conversation: conversation}, nil,
		operation.client.receipt(call, result.response, conversation.ID))
}

// reconcileEarlierDispatch never sends: it reports the conversation an earlier attempt created, or an unconfirmed write.
func (operation CreateConversationOperation) reconcileEarlierDispatch(call sdkgo.Call, credentials Credentials) reconciliation[CreateConversationOutput] {
	key := dispatchKey(call)
	readContext, cancel := reconciliationContext(call)
	defer cancel()
	result := operation.client.exchange(call, credentials, createConversationOperation, reamazeRequest{
		method: http.MethodGet, path: "/conversations", requestContext: readContext,
		query: url.Values{"filter": {string(ConversationFilterAll)}, "data[" + DispatchKeyDataAttribute + "]": {key}},
	})
	receipt := operation.client.receipt(call, result.response, "")
	if readBack, isTerminal := fencedReconciliationRead[CreateConversationOutput](result, receipt, createConversationOperation); isTerminal {
		return readBack
	}
	match, err := findConversationWithDispatchKey(result.response.body, key)
	if err != nil {
		return unconfirmedReconciliation[CreateConversationOutput](reamazeFailure(sdkgo.FailureProtocol, createConversationOperation,
			"an earlier attempt of this Step sent the request, and the conversation list read back is invalid: "+err.Error()), receipt)
	}
	if !match.isConfirmed && match.candidateSlug != "" {
		return operation.confirmDispatchCandidate(call, credentials, match.candidateSlug, key)
	}
	if !match.isConfirmed {
		return unconfirmedReconciliation[CreateConversationOutput](reamazeFailure(sdkgo.FailureTransport, createConversationOperation, uncertainNotFoundMessage), receipt)
	}
	return confirmedReconciliation(sdkgo.NewMutationBranch(CreateConversationBranchCreated, CreateConversationOutput{Conversation: match.conversation, WasAlreadyApplied: true}, nil,
		operation.client.receipt(call, result.response, match.conversation.ID)))
}

// confirmDispatchCandidate reads one listed conversation, because Re:amaze documents data only on single conversation reads.
func (operation CreateConversationOperation) confirmDispatchCandidate(call sdkgo.Call, credentials Credentials, slug string, key string) reconciliation[CreateConversationOutput] {
	readContext, cancel := reconciliationContext(call)
	defer cancel()
	result := operation.client.exchange(call, credentials, createConversationOperation, reamazeRequest{method: http.MethodGet, path: conversationPath(slug), requestContext: readContext})
	receipt := operation.client.receipt(call, result.response, slug)
	if readBack, isTerminal := fencedReconciliationRead[CreateConversationOutput](result, receipt, createConversationOperation); isTerminal {
		return readBack
	}
	conversation, isConfirmed, err := decodeConversationWithDispatchKey(result.response.body, slug, key)
	if err != nil {
		return unconfirmedReconciliation[CreateConversationOutput](reamazeFailure(sdkgo.FailureProtocol, createConversationOperation,
			"an earlier attempt of this Step sent the request, and the conversation read back is invalid: "+err.Error()), receipt)
	}
	if !isConfirmed {
		return unconfirmedReconciliation[CreateConversationOutput](reamazeFailure(sdkgo.FailureTransport, createConversationOperation, uncertainNotFoundMessage), receipt)
	}
	return confirmedReconciliation(sdkgo.NewMutationBranch(CreateConversationBranchCreated, CreateConversationOutput{Conversation: conversation, WasAlreadyApplied: true}, nil, receipt))
}

// findConversationWithDispatchKey verifies the key itself, so a list that ignored the data filter matches nothing.
func findConversationWithDispatchKey(body []byte, key string) (dispatchKeyMatch, error) {
	var document struct {
		pageWire
		Conversations *[]conversationWire `json:"conversations"`
	}
	if err := json.Unmarshal(body, &document); err != nil || document.Conversations == nil {
		return dispatchKeyMatch{}, errors.New("response is not a conversation page")
	}
	if err := document.pageWire.validate(len(*document.Conversations)); err != nil {
		return dispatchKeyMatch{}, err
	}
	match := dispatchKeyMatch{}
	for _, wire := range *document.Conversations {
		if wire.Data == nil && match.candidateSlug == "" && validateConversationID("slug", wire.Slug) == nil {
			match.candidateSlug = wire.Slug
		}
		if !hasDispatchKey(wire, key) {
			continue
		}
		conversation, err := decodeConversationWire(wire, true)
		return dispatchKeyMatch{conversation: conversation, isConfirmed: err == nil}, err
	}
	return match, nil
}

// decodeConversationWithDispatchKey decodes a single conversation read and reports whether its data holds key.
func decodeConversationWithDispatchKey(body []byte, slug string, key string) (Conversation, bool, error) {
	var wire conversationWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Conversation{}, false, errors.New("response is not a conversation object")
	}
	if !hasDispatchKey(wire, key) {
		return Conversation{}, false, nil
	}
	conversation, err := decodeConversationWire(wire, true)
	if err != nil {
		return Conversation{}, false, err
	}
	if conversation.ID != slug {
		return Conversation{}, false, errors.New("response is for another conversation")
	}
	return conversation, true, nil
}

func hasDispatchKey(wire conversationWire, key string) bool {
	var value string
	raw, isPresent := wire.Data[DispatchKeyDataAttribute]
	return isPresent && json.Unmarshal(raw, &value) == nil && value == key
}

func validateCreateConversationInput(input CreateConversationInput) error {
	if err := validateTextInput("subject", input.Subject); err != nil {
		return err
	}
	if err := validateTextInput("message", input.Message); err != nil {
		return err
	}
	if err := validateChannelSlug("channel", input.Channel); err != nil {
		return err
	}
	if !isBareEmailAddress(input.Requester.Email) {
		return errors.New("requester.email must be one bare email address such as jane@example.com")
	}
	if input.Requester.Name != strings.TrimSpace(input.Requester.Name) || len(input.Requester.Name) > 255 {
		return errors.New("requester.name must be at most 255 bytes without surrounding spaces")
	}
	if input.Status != nil {
		if err := validateConversationStatus("status", *input.Status); err != nil {
			return err
		}
	}
	if err := validateHoldUntil(input.HoldUntil, input.Status); err != nil {
		return err
	}
	if input.AssigneeEmail != "" && !isBareEmailAddress(input.AssigneeEmail) {
		return errors.New("assigneeEmail must be one bare staff email address such as agent@example.com")
	}
	return validateTags("tags", input.Tags)
}

func buildCreateConversationRequest(input CreateConversationInput, key string) createConversationRequestWire {
	conversation := createConversationWire{
		Subject: input.Subject, Category: input.Channel, TagList: input.Tags, HoldUntil: input.HoldUntil, Assignee: input.AssigneeEmail,
		Data:    map[string]string{DispatchKeyDataAttribute: key},
		Message: createConversationMessageWire{Body: input.Message, ShouldSuppressNotifications: input.ShouldSuppressNotifications},
		User:    participantRequestWire{Name: input.Requester.Name, Email: input.Requester.Email},
	}
	if input.Status != nil {
		status := int(*input.Status)
		conversation.Status = &status
	}
	return createConversationRequestWire{Conversation: conversation}
}
