// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const addNoteOperation = "addNote"

var addNoteDispatchBranches = singleDispatchBranches{
	notFound: AddNoteBranchNotFound, providerRejected: AddNoteBranchProviderRejected, defect: AddNoteBranchDefect,
}

// AddNoteInput is one internal note or one public email reply to add to a ticket.
type AddNoteInput struct {
	// TicketID is the Gorgias ticket ID.
	TicketID int64 `json:"ticketId"`
	// Body is the plain-text note or reply. It is required. The connector also sends it
	// HTML-escaped as body_html, so markup is shown literally and line breaks are kept.
	Body string `json:"body"`
	// IsPublicReply sends the body as an email that Gorgias delivers to the customer, to the
	// sender of the customer's newest email on the ticket, through the integration that received
	// it. The reply comes from the address an earlier agent email through that integration came
	// from, or else from that email's only To or CC address other than the customer's; when
	// neither identifies the helpdesk address, nothing is sent and providerRejected is selected.
	// False adds an internal note that only agents see.
	IsPublicReply bool `json:"isPublicReply,omitempty"`
}

// AddNoteOutput is the added note or reply.
type AddNoteOutput struct {
	// Message is the note or reply Gorgias added. A reply's SentAt is nil until Gorgias sends it.
	Message TicketMessage `json:"message"`
	// WasCreatedByEarlierAttempt reports that an earlier attempt of this Step added the message
	// and this attempt found it by its external ID instead of sending the request again.
	WasCreatedByEarlierAttempt bool `json:"wasCreatedByEarlierAttempt,omitempty"`
}

// AddNoteOperation is the addNote Mutation.
type AddNoteOperation struct {
	client *Client
}

type messagePersonWire struct {
	Email string `json:"email"`
}

type messageSourceWire struct {
	Type string               `json:"type"`
	From gorgiasAddressWire   `json:"from"`
	To   []gorgiasAddressWire `json:"to"`
}

type createMessageRequestWire struct {
	Channel       string             `json:"channel"`
	Via           string             `json:"via"`
	FromAgent     bool               `json:"from_agent"`
	Public        bool               `json:"public"`
	Sender        messagePersonWire  `json:"sender"`
	Receiver      *messagePersonWire `json:"receiver,omitempty"`
	Source        *messageSourceWire `json:"source,omitempty"`
	IntegrationID int64              `json:"integration_id,omitempty"`
	Subject       string             `json:"subject,omitempty"`
	BodyText      string             `json:"body_text"`
	BodyHTML      string             `json:"body_html"`
	ExternalID    string             `json:"external_id"`
}

// replyRoute answers the customer's newest email through the integration and helpdesk address it reached.
type replyRoute struct {
	integrationID   int64
	supportAddress  string
	customerAddress string
	subject         string
}

var (
	errNoCustomerEmail = errors.New("the ticket has no email from the customer to reply to; add an internal note instead")
	// errUnknownSupportAddress keeps a reply from going out from an address no Gorgias email integration owns.
	errUnknownSupportAddress = errors.New("the customer's newest email reached several addresses and none is known to be the helpdesk's, " +
		"so the reply is not sent; reply in Gorgias or add an internal note instead")
)

// Definition returns the immutable connector operation definition.
func (AddNoteOperation) Definition() sdkgo.MutationDefinition { return AddNoteDefinition }

// IdempotencyKey prefixes the stable connector Call ID with dex-. Gorgias documents no idempotency
// key, so the connector writes this key to the message's external_id and finds the message by it.
func (AddNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ AddNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey("dex-" + string(callID))
}

// Invoke sends POST /api/tickets/{id}/messages once per Step execution unless Gorgias provably did
// not apply it; a reply first reads the ticket to route it. A 429 or a connection that never opened
// is retried and may send again, after a later attempt first looks for the message by external_id;
// any other unconfirmed outcome is retried only to look for the message, because a repeated reply
// is a second email to the customer.
func (operation AddNoteOperation) Invoke(call sdkgo.Call, input AddNoteInput) sdkgo.MutationAttempt[AddNoteOutput] {
	if err := validateAddNoteInput(input); err != nil {
		return sdkgo.NewMutationBranch(AddNoteBranchDefect, AddNoteOutput{}, gorgiasFailurePointer(sdkgo.FailureValidation, addNoteOperation, err.Error()), sdkgo.Receipt{})
	}
	if hasEarlierDispatch(call) {
		credentials, failure := operation.client.resolveCredentials(call, addNoteOperation)
		if failure != nil {
			return reconcileCredentialFailureAttempt[AddNoteOutput](call, *failure, addNoteOperation)
		}
		return operation.findEarlierMessage(call, credentials, input.TicketID)
	}
	credentials, failure := operation.client.resolveCredentials(call, addNoteOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(AddNoteBranchDefect, AddNoteOutput{}, failure, sdkgo.Receipt{})
	}
	if isLaterAttempt(call) {
		message, isFound, result, receipt := operation.lookUpMessageByExternalID(call, credentials, input.TicketID)
		switch {
		case result.outcome != exchangeSucceeded:
			return unsentRequestAttempt[AddNoteOutput](result, receipt, addNoteDispatchBranches)
		case isFound:
			return sdkgo.NewMutationBranch(AddNoteBranchAdded, AddNoteOutput{Message: message, WasCreatedByEarlierAttempt: true}, nil, receipt)
		}
	}
	request := createMessageRequestWire{
		Channel: MessageChannelInternalNote, Via: MessageChannelAPI, FromAgent: true, Sender: messagePersonWire{Email: credentials.Email},
		BodyText: input.Body, BodyHTML: plainTextToGorgiasHTML(input.Body), ExternalID: string(call.IdempotencyKey),
	}
	if input.IsPublicReply {
		route, attempt, isTerminal := operation.findReplyRoute(call, credentials, input.TicketID)
		if isTerminal {
			return attempt
		}
		request.Channel, request.Public, request.Subject, request.IntegrationID = MessageChannelEmail, true, route.subject, route.integrationID
		request.Receiver = &messagePersonWire{Email: route.customerAddress}
		request.Source = &messageSourceWire{Type: MessageChannelEmail, From: gorgiasAddressWire{Address: route.supportAddress},
			To: []gorgiasAddressWire{{Address: route.customerAddress}}}
	}
	if recordDispatch(call) != nil {
		return dispatchNotRecordedAttempt[AddNoteOutput](addNoteOperation)
	}
	result := operation.client.exchange(call, credentials, addNoteOperation, gorgiasRequest{
		method: http.MethodPost, path: ticketPath(input.TicketID) + "/messages", payload: request,
	})
	receipt := operation.client.receipt(call, result.response, 0)
	if attempt, isTerminal := singleDispatchAttemptForSend[AddNoteOutput](call, result, receipt, addNoteDispatchBranches); isTerminal {
		return attempt
	}
	message, err := decodeAddedMessage(result.response.body, input.TicketID, string(call.IdempotencyKey))
	if err != nil {
		return sdkgo.NewMutationRetry[AddNoteOutput](gorgiasFailure(sdkgo.FailureProtocol, addNoteOperation,
			"Gorgias accepted the message but returned an invalid message: "+err.Error()), reconcileDelay)
	}
	return sdkgo.NewMutationBranch(AddNoteBranchAdded, AddNoteOutput{Message: message}, nil, operation.client.receipt(call, result.response, message.ID))
}

// findReplyRoute reads the ticket and answers its customer's newest email from the address that email reached.
func (operation AddNoteOperation) findReplyRoute(call sdkgo.Call, credentials Credentials, ticketID int64) (replyRoute, sdkgo.MutationAttempt[AddNoteOutput], bool) {
	result := operation.client.exchange(call, credentials, addNoteOperation, gorgiasRequest{method: http.MethodGet, path: ticketPath(ticketID)})
	receipt := operation.client.receipt(call, result.response, ticketID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return replyRoute{}, sdkgo.NewMutationRetry[AddNoteOutput](result.failure, result.retryAfter), true
	case exchangeNotFound:
		return replyRoute{}, sdkgo.NewMutationBranch(AddNoteBranchNotFound, AddNoteOutput{}, &result.failure, receipt), true
	case exchangeDefect:
		return replyRoute{}, sdkgo.NewMutationBranch(AddNoteBranchDefect, AddNoteOutput{}, &result.failure, receipt), true
	default:
		return replyRoute{}, sdkgo.NewMutationBranch(AddNoteBranchProviderRejected, AddNoteOutput{}, &result.failure, receipt), true
	}
	ticket, ticketWire, err := decodeTicketBody(result.response.body, ticketID)
	if err != nil {
		return replyRoute{}, sdkgo.NewMutationBranch(AddNoteBranchProviderRejected, AddNoteOutput{}, gorgiasFailurePointer(sdkgo.FailureProtocol, addNoteOperation,
			"Gorgias returned an invalid ticket, so the reply could not be routed: "+err.Error()), receipt), true
	}
	route, err := newestCustomerEmailRoute(ticketWire.Messages, ticket.Subject)
	if err != nil {
		return replyRoute{}, sdkgo.NewMutationBranch(AddNoteBranchProviderRejected, AddNoteOutput{}, gorgiasFailurePointer(sdkgo.FailureValidation, addNoteOperation,
			err.Error()), receipt), true
	}
	return route, sdkgo.MutationAttempt[AddNoteOutput]{}, false
}

// findEarlierMessage looks for the message an earlier attempt may have added, by the external ID it wrote.
func (operation AddNoteOperation) findEarlierMessage(call sdkgo.Call, credentials Credentials, ticketID int64) sdkgo.MutationAttempt[AddNoteOutput] {
	message, isFound, result, receipt := operation.lookUpMessageByExternalID(call, credentials, ticketID)
	switch {
	case result.outcome != exchangeSucceeded:
		return reconcileLookupFailureAttempt[AddNoteOutput](call, result, receipt, addNoteOperation)
	case isFound:
		return sdkgo.NewMutationBranch(AddNoteBranchAdded, AddNoteOutput{Message: message, WasCreatedByEarlierAttempt: true}, nil, receipt)
	default:
		return reconcileNotFoundAttempt[AddNoteOutput](call, receipt, addNoteOperation, "message")
	}
}

// lookUpMessageByExternalID reads the ticket's embedded messages; an unreadable ticket returns an exchangeInvalid result.
func (operation AddNoteOperation) lookUpMessageByExternalID(call sdkgo.Call, credentials Credentials, ticketID int64) (TicketMessage, bool, gorgiasExchange, sdkgo.Receipt) {
	externalID := string(call.IdempotencyKey)
	result := operation.client.exchange(call, credentials, addNoteOperation, gorgiasRequest{method: http.MethodGet, path: ticketPath(ticketID)})
	receipt := operation.client.receipt(call, result.response, ticketID)
	if result.outcome != exchangeSucceeded {
		return TicketMessage{}, false, result, receipt
	}
	_, ticketWire, err := decodeTicketBody(result.response.body, ticketID)
	if err != nil {
		return TicketMessage{}, false, gorgiasExchange{outcome: exchangeInvalid, response: result.response,
			failure: gorgiasFailure(sdkgo.FailureProtocol, addNoteOperation, "Gorgias returned an invalid ticket: "+err.Error())}, receipt
	}
	for _, wire := range ticketWire.Messages {
		if stringValue(wire.ExternalID) != externalID {
			continue
		}
		message, err := decodeMessageWire(wire)
		if err == nil {
			message.TicketID = ticketID
			return message, true, result, operation.client.receipt(call, result.response, message.ID)
		}
	}
	return TicketMessage{}, false, result, receipt
}

func validateAddNoteInput(input AddNoteInput) error {
	if input.TicketID < 1 {
		return errors.New("ticketId must be a positive Gorgias ticket ID")
	}
	return validateTextInput("body", input.Body)
}

// newestCustomerEmailRoute answers the newest customer email that names its sender and an address it reached.
func newestCustomerEmailRoute(messages []gorgiasMessageWire, ticketSubject string) (replyRoute, error) {
	var newest *gorgiasMessageWire
	for index := range messages {
		message := &messages[index]
		if message.FromAgent || message.Channel != MessageChannelEmail || message.Source == nil || message.Source.From == nil ||
			!isBareEmailAddress(message.Source.From.Address) || len(recipientAddresses(*message)) == 0 {
			continue
		}
		if newest == nil || isNewerMessage(*message, *newest) {
			newest = message
		}
	}
	if newest == nil {
		return replyRoute{}, errNoCustomerEmail
	}
	supportAddress, isKnown := supportAddressFor(*newest, messages)
	if !isKnown {
		return replyRoute{}, errUnknownSupportAddress
	}
	subject := stringValue(newest.Subject)
	if subject == "" {
		subject = ticketSubject
	}
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	route := replyRoute{supportAddress: supportAddress, customerAddress: newest.Source.From.Address, subject: subject}
	if newest.IntegrationID != nil {
		route.integrationID = *newest.IntegrationID
	}
	return route, nil
}

// supportAddressFor prefers the address an agent email through the same integration came from; otherwise the only recipient.
func supportAddressFor(customerEmail gorgiasMessageWire, messages []gorgiasMessageWire) (string, bool) {
	if customerEmail.IntegrationID != nil {
		var newestAgentEmail *gorgiasMessageWire
		for index := range messages {
			message := &messages[index]
			if !message.FromAgent || message.Channel != MessageChannelEmail || message.IntegrationID == nil ||
				*message.IntegrationID != *customerEmail.IntegrationID || message.Source == nil || message.Source.From == nil ||
				!isBareEmailAddress(message.Source.From.Address) {
				continue
			}
			if newestAgentEmail == nil || isNewerMessage(*message, *newestAgentEmail) {
				newestAgentEmail = message
			}
		}
		if newestAgentEmail != nil {
			return newestAgentEmail.Source.From.Address, true
		}
	}
	recipients := recipientAddresses(customerEmail)
	if len(recipients) != 1 {
		return "", false
	}
	return recipients[0], true
}

// recipientAddresses lists a customer email's distinct To and CC addresses, leaving out the customer's own.
func recipientAddresses(customerEmail gorgiasMessageWire) []string {
	var addresses []string
	for _, recipient := range slices.Concat(customerEmail.Source.To, customerEmail.Source.CC) {
		isListed := slices.ContainsFunc(addresses, func(address string) bool { return strings.EqualFold(address, recipient.Address) })
		if isListed || !isBareEmailAddress(recipient.Address) || strings.EqualFold(recipient.Address, customerEmail.Source.From.Address) {
			continue
		}
		addresses = append(addresses, recipient.Address)
	}
	return addresses
}

func isNewerMessage(candidate gorgiasMessageWire, current gorgiasMessageWire) bool {
	candidateAt, candidateErr := parseGorgiasTimestamp(candidate.CreatedDatetime)
	currentAt, currentErr := parseGorgiasTimestamp(current.CreatedDatetime)
	if candidateErr != nil || currentErr != nil || candidateAt.Equal(currentAt) {
		return candidate.ID > current.ID
	}
	return candidateAt.After(currentAt)
}

// decodeAddedMessage checks that Gorgias returned this Step's message on this ticket.
func decodeAddedMessage(body []byte, ticketID int64, externalID string) (TicketMessage, error) {
	var wire gorgiasMessageWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return TicketMessage{}, errors.New("message response is not a message object")
	}
	message, err := decodeMessageWire(wire)
	if err != nil {
		return TicketMessage{}, err
	}
	if message.TicketID != 0 && message.TicketID != ticketID {
		return TicketMessage{}, errors.New("message response is for another ticket")
	}
	if message.ExternalID != "" && message.ExternalID != externalID {
		return TicketMessage{}, errors.New("message response is another message")
	}
	message.TicketID = ticketID
	return message, nil
}
