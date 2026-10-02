// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package supportreply demonstrates every Outlook Mail operation in one Flow started from Dex Web Start
// Flow: find the customer's latest Inbox message, read it, send one threaded reply, mark the message read,
// and move it to the archive folder picked in Dex Web. A customer without a message gets one new message.
package supportreply

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "OutlookSupportReply"
	// ConnectionName is the static Dex Web connection for the support mailbox.
	ConnectionName = "outlook-support-mailbox"
	// DefaultArchiveFolder is the well-known folder used until an archive folder is picked in Dex Web.
	DefaultArchiveFolder = "archive"

	recordReplyRequestStepType          = "RecordReplyRequest"
	findCustomerMessagesStepType        = "FindCustomerMessages"
	chooseLatestCustomerMessageStepType = "ChooseLatestCustomerMessage"
	readCustomerMessageStepType         = "ReadCustomerMessage"
	prepareReplyStepType                = "PrepareReply"
	replyToCustomerStepType             = "ReplyToCustomer"
	recordReplySentStepType             = "RecordReplySent"
	markCustomerMessageReadStepType     = "MarkCustomerMessageRead"
	recordMessageMarkedStepType         = "RecordMessageMarked"
	archiveCustomerMessageStepType      = "ArchiveCustomerMessage"
	completeSupportReplyStepType        = "CompleteSupportReply"
	sendNewCustomerMessageStepType      = "SendNewCustomerMessage"
	recordNewMessageSentStepType        = "RecordNewMessageSent"
	recordUncertainDeliveryStepType     = "RecordUncertainDelivery"

	// customerSearchPageSize bounds each search page; later pages are read only when a page has no exact match.
	customerSearchPageSize = 10
)

var (
	replyRequestAttribute    = dex.DefineAttribute[ReplyRequest]("outlook-support-reply-request")
	customerMessageAttribute = dex.DefineAttribute[outlookmail.MessageSummary]("outlook-support-customer-message")
	replyOutcomeAttribute    = dex.DefineAttribute[ReplyOutcome]("outlook-support-reply-outcome")
)

// Input is the reply request entered in Dex Web Start Flow.
type Input struct {
	// CustomerEmail is the customer's bare address, such as jane@acme.example.com.
	CustomerEmail string `json:"customerEmail"`
	// ReplyText is the plain-text answer; Outlook quotes the customer's message below it.
	ReplyText string `json:"replyText"`
	// NewMessageSubject is the subject of a new message when the customer has no message in the Inbox.
	NewMessageSubject string `json:"newMessageSubject"`
}

// ReplyRequest is the validated request every later Step reads.
type ReplyRequest struct {
	// CustomerEmail is the customer's bare address.
	CustomerEmail string `json:"customerEmail"`
	// ReplyText is the plain-text answer.
	ReplyText string `json:"replyText"`
	// NewMessageSubject is the subject of a new message.
	NewMessageSubject string `json:"newMessageSubject"`
}

// ArchiveFolderSelection is the value the mail folder picker saves for the ArchiveCustomerMessage Step.
type ArchiveFolderSelection struct {
	// FolderID is the picked folder's ID; empty uses DefaultArchiveFolder.
	FolderID string `json:"folderId,omitempty"`
	// FolderName is the picked folder's display name.
	FolderName string `json:"folderName,omitempty"`
}

// CustomerMessageSearch is one page of the search for the customer's messages.
type CustomerMessageSearch struct {
	// CustomerEmail is the sender to search for.
	CustomerEmail string `json:"customerEmail"`
	// PageCursor continues the search, or is empty for the first page.
	PageCursor string `json:"pageCursor,omitempty"`
}

// CustomerReply is the reply to send to the customer's message.
type CustomerReply struct {
	// MessageID is the customer's message.
	MessageID string `json:"messageId"`
	// Text is the reply text.
	Text string `json:"text"`
}

// ReplyAction is what the Flow did.
type ReplyAction string

const (
	// ReplyActionReplied means the customer's latest message got one threaded reply.
	ReplyActionReplied ReplyAction = "replied"
	// ReplyActionSentNewMessage means the customer had no message in the Inbox, so a new message was sent.
	ReplyActionSentNewMessage ReplyAction = "sentNewMessage"
	// ReplyActionDeliveryUncertain means the send was dispatched but could not be confirmed.
	ReplyActionDeliveryUncertain ReplyAction = "deliveryUncertain"
)

// ReplyOutcome is the Flow result and the value of its outcome Attribute.
type ReplyOutcome struct {
	// Action is what the Flow did.
	Action ReplyAction `json:"action"`
	// CustomerMessageID is the customer's message that was answered, when there was one.
	CustomerMessageID string `json:"customerMessageId,omitempty"`
	// AttachmentCount is the number of attachments the customer's message listed.
	AttachmentCount int `json:"attachmentCount,omitempty"`
	// Sent is the reply or new message Graph accepted, or the draft that may have been sent.
	Sent outlookmail.SentMessage `json:"sent"`
	// Flags are the customer's message state after it was marked read.
	Flags *outlookmail.MessageFlags `json:"flags,omitempty"`
	// ArchivedTo is the folder that holds the answered message.
	ArchivedTo *outlookmail.MovedMessage `json:"archivedTo,omitempty"`
	// NeedsReview reports a send with an unknown outcome; a person must look for Sent.IdempotencyMarker or
	// open Sent.MessageID before anything is sent again.
	NeedsReview bool `json:"needsReview,omitempty"`
	// ReviewDetail is the connector's credential-free explanation of why the outcome is unknown.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// Flow answers one customer from an Outlook mailbox.
type Flow struct {
	dex.FlowDefaults
	connection      outlookmail.Connection
	archiveFolderID string
}

// NewFlow binds the Outlook Mail Connection and the archive folder loaded at startup. A blank selection
// uses DefaultArchiveFolder.
func NewFlow(connection outlookmail.Connection, archiveFolder ArchiveFolderSelection) *Flow {
	archiveFolderID := strings.TrimSpace(archiveFolder.FolderID)
	if archiveFolderID == "" {
		archiveFolderID = DefaultArchiveFolder
	}
	return &Flow{connection: connection, archiveFolderID: archiveFolderID}
}

// ArchiveFolderConfigurationRef identifies the archive folder picker value saved in Dex Web.
func ArchiveFolderConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: outlookmail.ConnectorID, ConnectionName: ConnectionName, OperationID: "moveMessage",
		FlowType: FlowType, StepType: archiveCustomerMessageStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Outlook Mail connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordReplyRequest{}),
		dex.DefineStep(outlookmail.NewSearchMessagesStep(outlookmail.SearchMessagesStepConfig[CustomerMessageSearch]{
			StepType: findCustomerMessagesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "outlook-mail", GroupLabel: "Outlook Mail",
				Explanation: "Search one page of the Inbox, newest first, for messages from the customer.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSearchMessagesInput,
			Searched: sdkgo.GoTo(chooseLatestCustomerMessage{}),
		})),
		dex.DefineStep(chooseLatestCustomerMessage{}),
		dex.DefineStep(outlookmail.NewGetMessageStep(outlookmail.GetMessageStepConfig[outlookmail.MessageSummary]{
			StepType: readCustomerMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "outlook-mail", GroupLabel: "Outlook Mail",
				Explanation: "Read the customer's latest message as plain text without marking it read.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetMessageInput,
			Found: sdkgo.GoTo(prepareReply{}),
		})),
		dex.DefineStep(prepareReply{}),
		dex.DefineStep(outlookmail.NewReplyToMessageStep(outlookmail.ReplyToMessageStepConfig[CustomerReply]{
			StepType: replyToCustomerStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "outlook-mail", GroupLabel: "Outlook Mail",
				Explanation: "Send one threaded reply through a marked draft, at most once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToReplyToMessageInput,
			Sent:      sdkgo.GoTo(recordReplySent{}),
			Uncertain: sdkgo.GoTo(recordUncertainDelivery{}),
		})),
		dex.DefineStep(recordReplySent{}),
		dex.DefineStep(outlookmail.NewSetMessageFlagsStep(outlookmail.SetMessageFlagsStepConfig[string]{
			StepType: markCustomerMessageReadStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "outlook-mail", GroupLabel: "Outlook Mail",
				Explanation: "Mark the customer's message read; a repeated attempt changes nothing twice.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSetMessageFlagsInput,
			Updated: sdkgo.GoTo(recordMessageMarked{}),
		})),
		dex.DefineStep(recordMessageMarked{archiveFolderID: flow.archiveFolderID}),
		dex.DefineStep(outlookmail.NewMoveMessageStep(outlookmail.MoveMessageStepConfig[outlookmail.MoveMessageInput]{
			StepType: archiveCustomerMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "outlook-mail", GroupLabel: "Outlook Mail",
				Explanation: "Move the answered message to the archive folder; a repeated attempt finds it already there.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "archiveFolder", UnitID: outlookmail.UIUnitMailFolderPicker, Label: "Archive folder",
				Description: "Choose the mail folder that receives each answered customer message, such as Archive or a Support > Answered subfolder; " +
					"the list shows the connection mailbox's folders, and the Step stores the folder's stable ID. The folder must already exist. " +
					"Leave it unsaved to use the mailbox's well-known Archive folder. Restart the Worker after saving.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: outlookmail.UIMailFolderPickerPortFolderID, JSONPointer: "/folderId"},
					{Port: outlookmail.UIMailFolderPickerPortFolderName, JSONPointer: "/folderName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: MapToMoveMessageInput,
			Moved: sdkgo.GoTo(completeSupportReply{}),
		})),
		dex.DefineStep(completeSupportReply{}),
		dex.DefineStep(outlookmail.NewSendMessageStep(outlookmail.SendMessageStepConfig[ReplyRequest]{
			StepType: sendNewCustomerMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "outlook-mail", GroupLabel: "Outlook Mail",
				Explanation: "Send one new message to a customer without an Inbox message, at most once.",
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
// dex:field attribute-key:outlook-support-reply-request value-type:json editable:false description:"Reply request"
// dex:field attribute-key:outlook-support-reply-outcome value-type:json editable:false description:"Reply outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	inspection, err := readInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"outlook-support-reply-request": inspection.request,
		"outlook-support-reply-outcome": inspection.outcome,
	}}, nil
}

// GetDexDisplay returns the request, the customer's message, and the outcome.
//
// dex:field attribute-key:outlook-support-reply-request value-type:json editable:false description:"Customer and reply text"
// dex:field attribute-key:outlook-support-customer-message value-type:json editable:false description:"The customer's message that was answered"
// dex:field attribute-key:outlook-support-reply-outcome value-type:json editable:false description:"Action, sent message, flags, archive folder, and review state"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	inspection, err := readInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"outlook-support-reply-request":    inspection.request,
		"outlook-support-customer-message": inspection.message,
		"outlook-support-reply-outcome":    inspection.outcome,
	}}, nil
}

// MapToSearchMessagesInput searches the Inbox for messages from exactly the customer's address.
func MapToSearchMessagesInput(search CustomerMessageSearch) outlookmail.SearchMessagesInput {
	return outlookmail.SearchMessagesInput{
		Folder: outlookmail.DefaultFolder, From: search.CustomerEmail, Limit: customerSearchPageSize, PageCursor: search.PageCursor,
	}
}

// MapToGetMessageInput reads the chosen message.
func MapToGetMessageInput(summary outlookmail.MessageSummary) outlookmail.GetMessageInput {
	return outlookmail.GetMessageInput{MessageID: summary.ID}
}

// MapToReplyToMessageInput replies to the sender only.
func MapToReplyToMessageInput(reply CustomerReply) outlookmail.ReplyToMessageInput {
	return outlookmail.ReplyToMessageInput{MessageID: reply.MessageID, Text: reply.Text}
}

// MapToSetMessageFlagsInput marks the message read.
func MapToSetMessageFlagsInput(messageID string) outlookmail.SetMessageFlagsInput {
	isRead := true
	return outlookmail.SetMessageFlagsInput{MessageID: messageID, IsRead: &isRead}
}

// MapToMoveMessageInput passes the prepared move through.
func MapToMoveMessageInput(move outlookmail.MoveMessageInput) outlookmail.MoveMessageInput {
	return move
}

// MapToSendMessageInput sends the reply text as a new message.
func MapToSendMessageInput(request ReplyRequest) outlookmail.SendMessageInput {
	return outlookmail.SendMessageInput{To: []string{request.CustomerEmail}, Subject: request.NewMessageSubject, Text: request.ReplyText}
}

// ChooseLatestCustomerMessage returns the newest summary sent by exactly the customer.
func ChooseLatestCustomerMessage(summaries []outlookmail.MessageSummary, customerEmail string) (outlookmail.MessageSummary, bool) {
	for _, summary := range summaries {
		if summary.From != nil && strings.EqualFold(summary.From.Address, customerEmail) {
			return summary, true
		}
	}
	return outlookmail.MessageSummary{}, false
}

// BuildReplyRequest validates Start Flow input so no connector Step receives an unusable request.
func BuildReplyRequest(input Input) (ReplyRequest, error) {
	request := ReplyRequest{
		CustomerEmail: strings.TrimSpace(input.CustomerEmail), ReplyText: strings.TrimSpace(input.ReplyText),
		NewMessageSubject: strings.TrimSpace(input.NewMessageSubject),
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
	return request, nil
}

// replyInspection is the Flow state the view RPCs show; an Attribute not yet written is its zero value.
type replyInspection struct {
	request ReplyRequest
	message outlookmail.MessageSummary
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
// dex:explanation text:"Validate the customer and reply text and record them before contacting Microsoft Graph."
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
	dex.StepDefaultsNoWaitFor[outlookmail.SearchMessagesResult]
}

func (chooseLatestCustomerMessage) GetStepType() string { return chooseLatestCustomerMessageStepType }

func (chooseLatestCustomerMessage) Execute(ctx dex.Context, result outlookmail.SearchMessagesResult) (*dex.StepDecision, error) {
	request, err := replyRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if summary, isFound := ChooseLatestCustomerMessage(result.Value.Messages, request.CustomerEmail); isFound {
		if err := customerMessageAttribute.Set(ctx, summary); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[outlookmail.MessageSummary](readCustomerMessageStepType), summary), nil
	}
	if result.Value.HasMore {
		return dex.GoTo(sdkgo.StepRef[CustomerMessageSearch](findCustomerMessagesStepType), CustomerMessageSearch{
			CustomerEmail: request.CustomerEmail, PageCursor: result.Value.NextPageCursor,
		}), nil
	}
	return dex.GoTo(sdkgo.StepRef[ReplyRequest](sendNewCustomerMessageStepType), request), nil
}

// dex:group group-id:reply group-label:"Threaded reply"
// dex:explanation text:"Record how many attachments the message lists and prepare the reply text."
type prepareReply struct {
	dex.StepDefaultsNoWaitFor[outlookmail.GetMessageResult]
}

func (prepareReply) GetStepType() string { return prepareReplyStepType }

func (prepareReply) Execute(ctx dex.Context, result outlookmail.GetMessageResult) (*dex.StepDecision, error) {
	request, err := replyRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ReplyOutcome{CustomerMessageID: result.Value.ID, AttachmentCount: len(result.Value.Attachments)}
	if err := replyOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CustomerReply](replyToCustomerStepType), CustomerReply{MessageID: result.Value.ID, Text: request.ReplyText}), nil
}

// dex:group group-id:reply group-label:"Threaded reply"
// dex:explanation text:"Record the accepted reply and mark the customer's message read."
type recordReplySent struct {
	dex.StepDefaultsNoWaitFor[outlookmail.ReplyToMessageResult]
}

func (recordReplySent) GetStepType() string { return recordReplySentStepType }

func (recordReplySent) Execute(ctx dex.Context, result outlookmail.ReplyToMessageResult) (*dex.StepDecision, error) {
	outcome, err := replyOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Action, outcome.Sent = ReplyActionReplied, result.Value
	if err := replyOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[string](markCustomerMessageReadStepType), outcome.CustomerMessageID), nil
}

// dex:group group-id:reply group-label:"Threaded reply"
// dex:explanation text:"Record the message state and archive the message in the picked folder."
type recordMessageMarked struct {
	dex.StepDefaultsNoWaitFor[outlookmail.SetMessageFlagsResult]
	archiveFolderID string
}

func (recordMessageMarked) GetStepType() string { return recordMessageMarkedStepType }

func (step recordMessageMarked) Execute(ctx dex.Context, result outlookmail.SetMessageFlagsResult) (*dex.StepDecision, error) {
	outcome, err := replyOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	flags := result.Value
	outcome.Flags = &flags
	if err := replyOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[outlookmail.MoveMessageInput](archiveCustomerMessageStepType), outlookmail.MoveMessageInput{
		MessageID: outcome.CustomerMessageID, DestinationFolder: step.archiveFolderID,
	}), nil
}

// dex:group group-id:reply group-label:"Threaded reply"
// dex:explanation text:"Record the folder that holds the answered message and complete the Flow."
type completeSupportReply struct {
	dex.StepDefaultsNoWaitFor[outlookmail.MoveMessageResult]
}

func (completeSupportReply) GetStepType() string { return completeSupportReplyStepType }

func (completeSupportReply) Execute(ctx dex.Context, result outlookmail.MoveMessageResult) (*dex.StepDecision, error) {
	outcome, err := replyOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	moved := result.Value
	outcome.ArchivedTo = &moved
	if err := replyOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:new-message group-label:"New message"
// dex:explanation text:"Record the accepted new message and complete the Flow."
type recordNewMessageSent struct {
	dex.StepDefaultsNoWaitFor[outlookmail.SendMessageResult]
}

func (recordNewMessageSent) GetStepType() string { return recordNewMessageSentStepType }

func (recordNewMessageSent) Execute(ctx dex.Context, result outlookmail.SendMessageResult) (*dex.StepDecision, error) {
	outcome := ReplyOutcome{Action: ReplyActionSentNewMessage, Sent: result.Value}
	if err := replyOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The send was dispatched with an unknown outcome; complete for a person to inspect the draft instead of sending again."
type recordUncertainDelivery struct {
	dex.StepDefaultsNoWaitFor[outlookmail.SendMessageResult]
}

func (recordUncertainDelivery) GetStepType() string { return recordUncertainDeliveryStepType }

func (recordUncertainDelivery) Execute(ctx dex.Context, result outlookmail.SendMessageResult) (*dex.StepDecision, error) {
	// prepareReply records an outcome only before a reply; a new message has none.
	outcome, err := replyOutcomeAttribute.Get(ctx)
	if err != nil && !isAttributeNotFound(err) {
		return nil, err
	}
	outcome.Action, outcome.Sent, outcome.NeedsReview, outcome.ReviewDetail = ReplyActionDeliveryUncertain, result.Value, true, failureMessage(result.Failure)
	if err := replyOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
