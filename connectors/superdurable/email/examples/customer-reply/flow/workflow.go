// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package customerreply demonstrates every email operation in one Flow started from Dex Web Start Flow:
// find the customer's latest message in INBOX, read it, send one threaded reply that quotes it, mark it
// seen and answered, and optionally move it to an archive mailbox. A customer without a message gets one
// new message instead.
package customerreply

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "EmailCustomerReply"
	// ConnectionName is the static Dex Web connection for the mailbox.
	ConnectionName = "email-mailbox"

	recordReplyRequestStepType          = "RecordReplyRequest"
	findCustomerMessagesStepType        = "FindCustomerMessages"
	chooseLatestCustomerMessageStepType = "ChooseLatestCustomerMessage"
	readCustomerMessageStepType         = "ReadCustomerMessage"
	prepareThreadedReplyStepType        = "PrepareThreadedReply"
	replyToCustomerStepType             = "ReplyToCustomer"
	recordReplySentStepType             = "RecordReplySent"
	markCustomerMessageAnsweredStepType = "MarkCustomerMessageAnswered"
	recordMessageMarkedStepType         = "RecordMessageMarked"
	archiveCustomerMessageStepType      = "ArchiveCustomerMessage"
	completeCustomerReplyStepType       = "CompleteCustomerReply"
	sendNewCustomerMessageStepType      = "SendNewCustomerMessage"
	recordNewMessageSentStepType        = "RecordNewMessageSent"
	recordUncertainDeliveryStepType     = "RecordUncertainDelivery"

	// customerSearchPageSize bounds each search page; the Flow reads later pages only when a page has no exact match.
	customerSearchPageSize = 10
	// maximumQuotedLines bounds the quotation of the customer's message in the reply.
	maximumQuotedLines = 20
)

var (
	replyRequestAttribute    = dex.DefineAttribute[ReplyRequest]("email-customer-reply-request")
	customerMessageAttribute = dex.DefineAttribute[email.MessageSummary]("email-customer-message")
	replyOutcomeAttribute    = dex.DefineAttribute[ReplyOutcome]("email-customer-reply-outcome")
)

// Input is the reply request entered in Dex Web Start Flow.
type Input struct {
	// CustomerEmail is the customer's bare address, such as jane@acme.example.com.
	CustomerEmail string `json:"customerEmail"`
	// ReplyText is the plain-text answer; the Flow adds a quotation of the customer's message.
	ReplyText string `json:"replyText"`
	// NewMessageSubject is the subject of a new message when the customer has no message in INBOX.
	NewMessageSubject string `json:"newMessageSubject"`
	// ArchiveMailbox is an existing mailbox, such as Archive, for the answered message; blank leaves it in INBOX.
	ArchiveMailbox string `json:"archiveMailbox,omitempty"`
}

// ReplyRequest is the validated request every later Step reads.
type ReplyRequest struct {
	// CustomerEmail is the customer's bare address.
	CustomerEmail string `json:"customerEmail"`
	// ReplyText is the plain-text answer.
	ReplyText string `json:"replyText"`
	// NewMessageSubject is the subject of a new message.
	NewMessageSubject string `json:"newMessageSubject"`
	// ArchiveMailbox is the archive mailbox, or empty.
	ArchiveMailbox string `json:"archiveMailbox,omitempty"`
}

// CustomerMessageSearch is one page of the search for the customer's messages.
type CustomerMessageSearch struct {
	// CustomerEmail is the sender to search for.
	CustomerEmail string `json:"customerEmail"`
	// OlderThanUID continues the search below this UID, or zero for the first page.
	OlderThanUID uint32 `json:"olderThanUid,omitempty"`
	// UIDValidity is the mailbox UIDVALIDITY of a continued search.
	UIDValidity uint32 `json:"uidValidity,omitempty"`
}

// ThreadedReply is the reply to send to the customer's message.
type ThreadedReply struct {
	// Message is the customer's message.
	Message email.MessageReference `json:"message"`
	// Text is the reply with its quotation.
	Text string `json:"text"`
}

// ArchiveRequest moves the answered message.
type ArchiveRequest struct {
	// Message is the customer's message.
	Message email.MessageReference `json:"message"`
	// DestinationMailbox is the archive mailbox.
	DestinationMailbox string `json:"destinationMailbox"`
	// MessageID lets a repeated move recognize the message in the archive.
	MessageID string `json:"messageId,omitempty"`
}

// ReplyAction is what the Flow did.
type ReplyAction string

const (
	// ReplyActionReplied means the customer's latest message got a threaded reply.
	ReplyActionReplied ReplyAction = "replied"
	// ReplyActionSentNewMessage means the customer had no message in INBOX, so a new message was sent.
	ReplyActionSentNewMessage ReplyAction = "sentNewMessage"
	// ReplyActionDeliveryUncertain means the message was submitted but the server's answer was lost.
	ReplyActionDeliveryUncertain ReplyAction = "deliveryUncertain"
)

// ReplyOutcome is the Flow result and the value of its outcome Attribute.
type ReplyOutcome struct {
	// Action is what the Flow did.
	Action ReplyAction `json:"action"`
	// CustomerMessage identifies the customer's message that was answered, when there was one.
	CustomerMessage *email.MessageReference `json:"customerMessage,omitempty"`
	// CustomerMessageID is that message's Message-ID.
	CustomerMessageID string `json:"customerMessageId,omitempty"`
	// Sent is the reply or new message the SMTP server accepted, or the one that may have been sent.
	Sent email.SentMessage `json:"sent"`
	// Flags are the customer's message flags after the reply.
	Flags *email.MessageFlags `json:"flags,omitempty"`
	// ArchivedTo identifies the message in the archive mailbox after the move.
	ArchivedTo *email.MessageReference `json:"archivedTo,omitempty"`
	// NeedsReview reports a submission with an unknown outcome; a person must check the Sent mailbox for
	// Sent.MessageID before anything is sent again.
	NeedsReview bool `json:"needsReview,omitempty"`
	// ReviewDetail is the connector's credential-free explanation of why the outcome is unknown.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// Flow answers one customer by email.
type Flow struct {
	dex.FlowDefaults
	connection email.Connection
}

// NewFlow binds the email Connection at registration time.
func NewFlow(connection email.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and email connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordReplyRequest{}),
		dex.DefineStep(email.NewSearchMessagesStep(email.SearchMessagesStepConfig[CustomerMessageSearch]{
			StepType: findCustomerMessagesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "email", GroupLabel: "Email",
				Explanation: "Search one page of INBOX, newest first, for messages from the customer.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSearchMessagesInput,
			Searched: sdkgo.GoTo(chooseLatestCustomerMessage{}),
		})),
		dex.DefineStep(chooseLatestCustomerMessage{}),
		dex.DefineStep(email.NewGetMessageStep(email.GetMessageStepConfig[email.MessageReference]{
			StepType: readCustomerMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "email", GroupLabel: "Email",
				Explanation: "Read the customer's latest message without marking it seen.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetMessageInput,
			Found: sdkgo.GoTo(prepareThreadedReply{}),
		})),
		dex.DefineStep(prepareThreadedReply{}),
		dex.DefineStep(email.NewReplyToMessageStep(email.ReplyToMessageStepConfig[ThreadedReply]{
			StepType: replyToCustomerStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "email", GroupLabel: "Email",
				Explanation: "Send one threaded reply over SMTP, submitting it at most once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToReplyToMessageInput,
			Sent:      sdkgo.GoTo(recordReplySent{}),
			Uncertain: sdkgo.GoTo(recordUncertainDelivery{}),
		})),
		dex.DefineStep(recordReplySent{}),
		dex.DefineStep(email.NewSetFlagsStep(email.SetFlagsStepConfig[email.MessageReference]{
			StepType: markCustomerMessageAnsweredStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "email", GroupLabel: "Email",
				Explanation: "Mark the customer's message seen and answered; a repeated attempt changes nothing twice.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSetFlagsInput,
			Updated: sdkgo.GoTo(recordMessageMarked{}),
		})),
		dex.DefineStep(recordMessageMarked{}),
		dex.DefineStep(email.NewMoveMessageStep(email.MoveMessageStepConfig[ArchiveRequest]{
			StepType: archiveCustomerMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "email", GroupLabel: "Email",
				Explanation: "Move the answered message to the archive mailbox; a repeated attempt finds it already moved.",
			},
			Connection: flow.connection, MapToOperationInput: MapToMoveMessageInput,
			Moved: sdkgo.GoTo(completeCustomerReply{}),
		})),
		dex.DefineStep(completeCustomerReply{}),
		dex.DefineStep(email.NewSendMessageStep(email.SendMessageStepConfig[ReplyRequest]{
			StepType: sendNewCustomerMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "email", GroupLabel: "Email",
				Explanation: "Send one new message to a customer without a message in INBOX, submitting it at most once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSendMessageInput,
			Sent:      sdkgo.GoTo(recordNewMessageSent{}),
			Uncertain: sdkgo.GoTo(recordUncertainDelivery{}),
		})),
		dex.DefineStep(recordNewMessageSent{}),
		dex.DefineStep(recordUncertainDelivery{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request, customer message, and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{replyRequestAttribute, customerMessageAttribute, replyOutcomeAttribute}}
}

// GetDexSummary returns the request and its outcome.
//
// dex:field attribute-key:email-customer-reply-request value-type:json editable:false description:"Reply request"
// dex:field attribute-key:email-customer-reply-outcome value-type:json editable:false description:"Reply outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	inspection, err := readInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"email-customer-reply-request": inspection.request,
		"email-customer-reply-outcome": inspection.outcome,
	}}, nil
}

// GetDexDisplay returns the request, the customer's message, and the outcome.
//
// dex:field attribute-key:email-customer-reply-request value-type:json editable:false description:"Customer, reply text, and archive mailbox"
// dex:field attribute-key:email-customer-message value-type:json editable:false description:"The customer's message that was answered"
// dex:field attribute-key:email-customer-reply-outcome value-type:json editable:false description:"Action, sent Message-ID, flags, archive, and review state"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	inspection, err := readInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"email-customer-reply-request": inspection.request,
		"email-customer-message":       inspection.message,
		"email-customer-reply-outcome": inspection.outcome,
	}}, nil
}

// MapToSearchMessagesInput searches INBOX for messages whose From header contains the customer's address.
func MapToSearchMessagesInput(search CustomerMessageSearch) email.SearchMessagesInput {
	return email.SearchMessagesInput{
		Mailbox: email.DefaultMailbox, From: search.CustomerEmail, Limit: customerSearchPageSize,
		OlderThanUID: search.OlderThanUID, UIDValidity: search.UIDValidity,
	}
}

// MapToGetMessageInput reads the chosen message.
func MapToGetMessageInput(reference email.MessageReference) email.GetMessageInput {
	return email.GetMessageInput{Message: reference}
}

// MapToReplyToMessageInput replies to the sender only.
func MapToReplyToMessageInput(reply ThreadedReply) email.ReplyToMessageInput {
	return email.ReplyToMessageInput{Message: reply.Message, Text: reply.Text}
}

// MapToSetFlagsInput marks the message seen and answered.
func MapToSetFlagsInput(reference email.MessageReference) email.SetFlagsInput {
	isSet := true
	return email.SetFlagsInput{Message: reference, IsSeen: &isSet, IsAnswered: &isSet}
}

// MapToMoveMessageInput moves the message with its Message-ID, so a repeated move is recognized.
func MapToMoveMessageInput(archive ArchiveRequest) email.MoveMessageInput {
	return email.MoveMessageInput{Message: archive.Message, DestinationMailbox: archive.DestinationMailbox, MessageID: archive.MessageID}
}

// MapToSendMessageInput sends the reply text as a new message.
func MapToSendMessageInput(request ReplyRequest) email.SendMessageInput {
	return email.SendMessageInput{To: []string{request.CustomerEmail}, Subject: request.NewMessageSubject, Text: request.ReplyText}
}

// ChooseLatestCustomerMessage returns the newest summary sent by exactly the customer. IMAP SEARCH FROM
// matches a substring, so jane@acme.example.com.au also matches jane@acme.example.com and is skipped here.
func ChooseLatestCustomerMessage(summaries []email.MessageSummary, customerEmail string) (email.MessageSummary, bool) {
	for _, summary := range summaries {
		if strings.EqualFold(summary.From.Address, customerEmail) {
			return summary, true
		}
	}
	return email.MessageSummary{}, false
}

// BuildThreadedReplyText adds a bounded quotation of the customer's message below the reply.
func BuildThreadedReplyText(replyText string, message email.Message) string {
	sender := message.From.Address
	if message.From.Name != "" {
		sender = message.From.Name + " <" + message.From.Address + ">"
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(message.Text, "\r\n", "\n")), "\n")
	if len(lines) > maximumQuotedLines {
		lines = append(lines[:maximumQuotedLines], "...")
	}
	var quotation strings.Builder
	for _, line := range lines {
		quotation.WriteString("> " + line + "\n")
	}
	written := "On " + message.ReceivedAt.UTC().Format("Mon, 2 Jan 2006 at 15:04 UTC") + ", " + sender + " wrote:"
	return strings.TrimSpace(replyText) + "\n\n" + written + "\n" + quotation.String()
}

// BuildReplyRequest validates Start Flow input so no connector Step receives an unusable request.
func BuildReplyRequest(input Input) (ReplyRequest, error) {
	request := ReplyRequest{
		CustomerEmail: strings.TrimSpace(input.CustomerEmail), ReplyText: strings.TrimSpace(input.ReplyText),
		NewMessageSubject: strings.TrimSpace(input.NewMessageSubject), ArchiveMailbox: strings.TrimSpace(input.ArchiveMailbox),
	}
	address, err := mail.ParseAddress(request.CustomerEmail)
	if err != nil || address.Name != "" || address.Address != request.CustomerEmail {
		return ReplyRequest{}, fmt.Errorf("customerEmail %q must be one bare address such as jane@acme.example.com", input.CustomerEmail)
	}
	if request.ReplyText == "" {
		return ReplyRequest{}, errors.New("replyText is required")
	}
	if request.NewMessageSubject == "" || strings.ContainsAny(request.NewMessageSubject, "\r\n") {
		return ReplyRequest{}, errors.New("newMessageSubject is required and must be one line")
	}
	if strings.EqualFold(request.ArchiveMailbox, email.DefaultMailbox) {
		return ReplyRequest{}, errors.New("archiveMailbox must differ from INBOX; leave it blank to keep the message in INBOX")
	}
	return request, nil
}

// replyInspection is the Flow state the view RPCs show; an Attribute not yet written is its zero value.
type replyInspection struct {
	request ReplyRequest
	message email.MessageSummary
	outcome ReplyOutcome
}

func readInspection(ctx dex.Context) (replyInspection, error) {
	var inspection replyInspection
	var err error
	if inspection.request, err = replyRequestAttribute.Get(ctx); err != nil && !isAttributeNotFound(err) {
		return replyInspection{}, err
	}
	if inspection.message, err = customerMessageAttribute.Get(ctx); err != nil && !isAttributeNotFound(err) {
		return replyInspection{}, err
	}
	if inspection.outcome, err = replyOutcomeAttribute.Get(ctx); err != nil && !isAttributeNotFound(err) {
		return replyInspection{}, err
	}
	return inspection, nil
}

func isAttributeNotFound(err error) bool {
	var missingAttribute *dex.AttributeNotFoundError
	return errors.As(err, &missingAttribute)
}

func failureMessage(failure *sdkgo.Failure) string {
	if failure == nil {
		return ""
	}
	return failure.Message
}

// dex:group group-id:request group-label:"Reply request"
// dex:explanation text:"Validate the customer and reply text and record them before contacting the mail servers."
type recordReplyRequest struct {
	dex.StepDefaults
}

func (recordReplyRequest) GetStepType() string { return recordReplyRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordReplyRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordReplyRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildReplyRequest(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := replyRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CustomerMessageSearch](findCustomerMessagesStepType), CustomerMessageSearch{CustomerEmail: request.CustomerEmail}), nil
}

// dex:group group-id:request group-label:"Reply request"
// dex:explanation text:"Pick the newest message sent by exactly the customer, search the next page when this page has none, or send a new message after the last page."
type chooseLatestCustomerMessage struct {
	dex.StepDefaultsNoWaitFor[email.SearchMessagesResult]
}

func (chooseLatestCustomerMessage) GetStepType() string { return chooseLatestCustomerMessageStepType }

func (chooseLatestCustomerMessage) Execute(ctx dex.Context, result email.SearchMessagesResult) (*dex.StepDecision, error) {
	request, err := replyRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if summary, isFound := ChooseLatestCustomerMessage(result.Value.Messages, request.CustomerEmail); isFound {
		if err := customerMessageAttribute.Set(ctx, summary); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[email.MessageReference](readCustomerMessageStepType), summary.Reference), nil
	}
	if result.Value.HasMore {
		return dex.GoTo(sdkgo.StepRef[CustomerMessageSearch](findCustomerMessagesStepType), CustomerMessageSearch{
			CustomerEmail: request.CustomerEmail, OlderThanUID: result.Value.NextOlderThanUID, UIDValidity: result.Value.UIDValidity,
		}), nil
	}
	return dex.GoTo(sdkgo.StepRef[ReplyRequest](sendNewCustomerMessageStepType), request), nil
}

// dex:group group-id:reply group-label:"Threaded reply"
// dex:explanation text:"Quote the customer's message below the reply text."
type prepareThreadedReply struct {
	dex.StepDefaultsNoWaitFor[email.GetMessageResult]
}

func (prepareThreadedReply) GetStepType() string { return prepareThreadedReplyStepType }

func (prepareThreadedReply) Execute(ctx dex.Context, result email.GetMessageResult) (*dex.StepDecision, error) {
	request, err := replyRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ThreadedReply](replyToCustomerStepType), ThreadedReply{
		Message: result.Value.Reference, Text: BuildThreadedReplyText(request.ReplyText, result.Value),
	}), nil
}

// dex:group group-id:reply group-label:"Threaded reply"
// dex:explanation text:"Record the accepted reply and mark the customer's message answered."
type recordReplySent struct {
	dex.StepDefaultsNoWaitFor[email.ReplyToMessageResult]
}

func (recordReplySent) GetStepType() string { return recordReplySentStepType }

func (recordReplySent) Execute(ctx dex.Context, result email.ReplyToMessageResult) (*dex.StepDecision, error) {
	message, err := customerMessageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	reference := message.Reference
	outcome := ReplyOutcome{Action: ReplyActionReplied, CustomerMessage: &reference, CustomerMessageID: message.MessageID, Sent: result.Value}
	if err := replyOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[email.MessageReference](markCustomerMessageAnsweredStepType), reference), nil
}

// dex:group group-id:reply group-label:"Threaded reply"
// dex:explanation text:"Record the message flags, then archive the message or complete."
type recordMessageMarked struct {
	dex.StepDefaultsNoWaitFor[email.SetFlagsResult]
}

func (recordMessageMarked) GetStepType() string { return recordMessageMarkedStepType }

func (recordMessageMarked) Execute(ctx dex.Context, result email.SetFlagsResult) (*dex.StepDecision, error) {
	request, err := replyRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := replyOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	flags := result.Value
	outcome.Flags = &flags
	if err := replyOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	if request.ArchiveMailbox == "" {
		return dex.GracefulComplete(outcome), nil
	}
	return dex.GoTo(sdkgo.StepRef[ArchiveRequest](archiveCustomerMessageStepType), ArchiveRequest{
		Message: *outcome.CustomerMessage, DestinationMailbox: request.ArchiveMailbox, MessageID: outcome.CustomerMessageID,
	}), nil
}

// dex:group group-id:reply group-label:"Threaded reply"
// dex:explanation text:"Record where the answered message was archived and complete the Flow."
type completeCustomerReply struct {
	dex.StepDefaultsNoWaitFor[email.MoveMessageResult]
}

func (completeCustomerReply) GetStepType() string { return completeCustomerReplyStepType }

func (completeCustomerReply) Execute(ctx dex.Context, result email.MoveMessageResult) (*dex.StepDecision, error) {
	outcome, err := replyOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	destination := result.Value.Destination
	outcome.ArchivedTo = &destination
	if err := replyOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:new-message group-label:"New message"
// dex:explanation text:"Record the accepted new message and complete the Flow."
type recordNewMessageSent struct {
	dex.StepDefaultsNoWaitFor[email.SendMessageResult]
}

func (recordNewMessageSent) GetStepType() string { return recordNewMessageSentStepType }

func (recordNewMessageSent) Execute(ctx dex.Context, result email.SendMessageResult) (*dex.StepDecision, error) {
	outcome := ReplyOutcome{Action: ReplyActionSentNewMessage, Sent: result.Value}
	if err := replyOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The message was submitted with an unknown outcome; complete for a person to look for its Message-ID instead of sending it again."
type recordUncertainDelivery struct {
	dex.StepDefaultsNoWaitFor[email.SendMessageResult]
}

func (recordUncertainDelivery) GetStepType() string { return recordUncertainDeliveryStepType }

func (recordUncertainDelivery) Execute(ctx dex.Context, result email.SendMessageResult) (*dex.StepDecision, error) {
	outcome := ReplyOutcome{Action: ReplyActionDeliveryUncertain, Sent: result.Value, NeedsReview: true, ReviewDetail: failureMessage(result.Failure)}
	// chooseLatestCustomerMessage records a message only before a reply; a new message has none.
	message, err := customerMessageAttribute.Get(ctx)
	if err != nil && !isAttributeNotFound(err) {
		return nil, err
	}
	if err == nil {
		reference := message.Reference
		outcome.CustomerMessage, outcome.CustomerMessageID = &reference, message.MessageID
	}
	if err := replyOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
