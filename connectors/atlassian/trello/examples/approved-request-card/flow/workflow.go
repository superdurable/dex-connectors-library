// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package approvedrequestcard demonstrates every Trello operation in one Flow started from Dex Web Start
// Flow: turn an approved request into a Trello card in the approved list, and comment with the approval. In
// Trello the list is the card's status, so a card that already carries the request ID is moved to the approved
// list instead of being created again, and an uncertain create is parked for an operator.
package approvedrequestcard

import (
	"errors"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "TrelloApprovedRequestCard"
	// ConnectionName is the static Dex Web connection for Trello.
	ConnectionName = "trello-requests"
	// ReconcileRequestCardPermission is required by both reconciliation Actions.
	ReconcileRequestCardPermission = "trello-request-card.reconcile"

	recordApprovedRequestStepType     = "RecordApprovedRequest"
	findOpenRequestCardsStepType      = "FindOpenRequestCards"
	evaluateOpenRequestCardsStepType  = "EvaluateOpenRequestCards"
	createRequestCardStepType         = "CreateRequestCard"
	recordCreatedCardStepType         = "RecordCreatedCard"
	recordRejectedCardStepType        = "RecordRejectedCard"
	recordUncertainCardStepType       = "RecordUncertainCard"
	readReportedCardStepType          = "ReadReportedCard"
	verifyReportedCardStepType        = "VerifyReportedCard"
	recordMissingReportedCardStepType = "RecordMissingReportedCard"
	moveRequestCardStepType           = "MoveRequestCard"
	recordMovedCardStepType           = "RecordMovedCard"
	recordRejectedMoveStepType        = "RecordRejectedMove"
	commentOnRequestCardStepType      = "CommentOnRequestCard"
	recordApprovalCommentStepType     = "RecordApprovalComment"

	// DefaultOpenCardPageSize is the number of open cards each duplicate-check page reads.
	DefaultOpenCardPageSize = 100
	// DefaultMaximumOpenCardPages bounds the duplicate check to 500 open cards on the board.
	DefaultMaximumOpenCardPages = 5
	maximumRequestIDSize        = 64
)

// Request card phases stored in the trello-request-card-phase Attribute.
const (
	// PhaseSearching means the Flow is looking for an open card that already carries the request ID.
	PhaseSearching = "searching"
	// PhaseCreating means a createCard Step is about to run or running.
	PhaseCreating = "creating"
	// PhaseMoving means the Flow is moving an existing card to the approved list.
	PhaseMoving = "moving"
	// PhaseCommenting means the Flow is adding the approval comment.
	PhaseCommenting = "commenting"
	// PhaseReady means the card is in the approved list and carries the approval comment.
	PhaseReady = "ready"
	// PhaseRejected means Trello conclusively rejected the create and nothing was created.
	PhaseRejected = "rejected"
	// PhaseMoveRejected means the card exists but Trello rejected the move, due date, or labels.
	PhaseMoveRejected = "moveRejected"
	// PhaseNeedsReconciliation means the create outcome is unknown and an operator must confirm or retry.
	PhaseNeedsReconciliation = "needsReconciliation"
	// PhaseVerifyingReportedCard means the Flow is reading the card an operator reported.
	PhaseVerifyingReportedCard = "verifyingReportedCard"
	// PhaseDuplicateCheckIncomplete means the board has more open cards than the duplicate check reads.
	PhaseDuplicateCheckIncomplete = "duplicateCheckIncomplete"
)

// Reconciliation notes explain why a reported card ID was not adopted.
const (
	// NoteReportedCardMissing means Trello has no card with the reported ID.
	NoteReportedCardMissing = "Trello has no card with the reported ID"
	// NoteReportedCardMismatch means the reported card is on another board or lacks the request ID.
	NoteReportedCardMismatch = "the reported card is on another board or its name lacks the request ID"
)

var (
	requestCardPhaseAttribute = dex.DefineAttribute[string]("trello-request-card-phase")
	requestCardAttribute      = dex.DefineAttribute[RequestCard]("trello-request-card")
)

var errReportedCardIDInvalid = errors.New("cardId must be a Trello card ID, 24 hexadecimal characters")

// Input is the approved request entered in Dex Web Start Flow.
type Input struct {
	// RequestID is the approved request's stable ID, such as REQ-1042; the card name starts with it in brackets.
	RequestID string `json:"requestId"`
	// Title is the request title, such as Replace badge reader at door 4.
	Title string `json:"title"`
	// Details is the plain-text card description.
	Details string `json:"details,omitempty"`
	// ApprovedBy names the approver for the approval comment.
	ApprovedBy string `json:"approvedBy"`
	// ApprovalNote is added to the approval comment; blank adds nothing.
	ApprovalNote string `json:"approvalNote,omitempty"`
	// BoardID is the Trello board searched for an existing request card.
	BoardID string `json:"boardId"`
	// ListID is the approved list on that board, which holds approved request cards.
	ListID string `json:"listId"`
	// DueAt is when the request is due, such as 2026-10-15T17:00:00Z; nil leaves the due date unchanged.
	DueAt *time.Time `json:"dueAt,omitempty"`
	// LabelIDs are board labels every request card carries, such as a Compliance label.
	LabelIDs []string `json:"labelIds,omitempty"`
	// MemberIDs are board members assigned to a new card; an existing card keeps its members.
	MemberIDs []string `json:"memberIds,omitempty"`
}

// DuplicateCheck bounds the search for an open card that already carries the request ID.
type DuplicateCheck struct {
	// PageSize is the number of open cards each listCards page reads, 1 to 100; zero uses 100.
	PageSize int
	// MaximumPages is the number of pages read before the Flow stops as duplicateCheckIncomplete; zero uses 5.
	MaximumPages int
}

// CardRequest is the validated request every later Step reads.
type CardRequest struct {
	// RequestID is the approved request's stable ID.
	RequestID string `json:"requestId"`
	// CardName is the card name: the request ID in brackets, then the title.
	CardName string `json:"cardName"`
	// Description is the plain-text card description.
	Description string `json:"description,omitempty"`
	// BoardID is the board that holds request cards.
	BoardID string `json:"boardId"`
	// ListID is the approved list.
	ListID string `json:"listId"`
	// Due is when the request is due, or nil.
	Due *time.Time `json:"due,omitempty"`
	// LabelIDs are the labels a request card carries.
	LabelIDs []string `json:"labelIds,omitempty"`
	// MemberIDs are the members assigned to a new card.
	MemberIDs []string `json:"memberIds,omitempty"`
	// Comment is the approval comment text.
	Comment string `json:"comment"`
}

// OpenCardPage selects one page of the board's open cards for the duplicate check.
type OpenCardPage struct {
	// BoardID is the board to list.
	BoardID string `json:"boardId"`
	// PageSize is the number of cards to read.
	PageSize int `json:"pageSize"`
	// Before continues with cards older than this card ID; blank reads the newest.
	Before string `json:"before,omitempty"`
}

// CardReference identifies the card an operator reported.
type CardReference struct {
	// CardID is the reported card's Trello ID.
	CardID string `json:"cardId"`
}

// CardMove is one updateCard call: reopen the card, move it to the approved list, set the due date, and add
// the request labels.
type CardMove struct {
	// CardID is the card to update.
	CardID string `json:"cardId"`
	// ListID is the approved list.
	ListID string `json:"listId"`
	// Due is the due date, or nil to leave it.
	Due *time.Time `json:"due,omitempty"`
	// LabelIDs are added to the card's labels.
	LabelIDs []string `json:"labelIds,omitempty"`
}

// CommentRequest is one addComment call.
type CommentRequest struct {
	// CardID is the card to comment on.
	CardID string `json:"cardId"`
	// Text is the plain-text comment.
	Text string `json:"text"`
}

// UncertainCreate records a dispatched create whose outcome Trello did not confirm.
type UncertainCreate struct {
	// CallID is the connector call identity of the uncertain create.
	CallID string `json:"callId"`
	// ObservedAt is when the connector observed the unknown outcome; look for the card around it.
	ObservedAt time.Time `json:"observedAt"`
	// FailureKind is the safe connector failure category, such as TRANSPORT.
	FailureKind sdkgo.FailureKind `json:"failureKind"`
	// FailureMessage is the connector's safe description, which never holds Trello text.
	FailureMessage string `json:"failureMessage,omitempty"`
}

// RequestCard is the Flow's durable record of one approved request's card.
type RequestCard struct {
	// Request is the validated request.
	Request CardRequest `json:"request"`
	// Phase mirrors the trello-request-card-phase Attribute.
	Phase string `json:"phase"`
	// OpenCardPagesRead counts listCards pages read by the duplicate check.
	OpenCardPagesRead int `json:"openCardPagesRead"`
	// CreateAttempts counts createCard Step executions: the first create plus each approved retry.
	CreateAttempts int `json:"createAttempts"`
	// IsExistingCardReused reports that the duplicate check found an open card with the request ID.
	IsExistingCardReused bool `json:"isExistingCardReused,omitempty"`
	// CardID is the card's Trello ID once it is known.
	CardID string `json:"cardId,omitempty"`
	// CardURL is the card's Trello web URL once it is known.
	CardURL string `json:"cardUrl,omitempty"`
	// Card is the card read back after a move.
	Card *trello.Card `json:"card,omitempty"`
	// CommentID is the approval comment's action ID once Trello confirmed it.
	CommentID string `json:"commentId,omitempty"`
	// IsCommentOutcomeUnknown reports a comment Trello may or may not have stored; it is never re-sent.
	IsCommentOutcomeUnknown bool `json:"isCommentOutcomeUnknown,omitempty"`
	// UncertainCreate describes the latest uncertain create while reconciliation is pending.
	UncertainCreate *UncertainCreate `json:"uncertainCreate,omitempty"`
	// ReconciliationNote explains why a reported card ID was not adopted.
	ReconciliationNote string `json:"reconciliationNote,omitempty"`
}

// ConfirmCreatedCardInput reports the card an operator found after an uncertain create.
type ConfirmCreatedCardInput struct {
	// CardID is the 24-character ID of the card found on the board.
	CardID string `json:"cardId"`
}

// Flow turns one approved request into a Trello card in the approved list with an approval comment.
type Flow struct {
	dex.FlowDefaults
	connection     trello.Connection
	duplicateCheck DuplicateCheck
}

// NewFlow binds the Trello Connection and the duplicate-check bounds at registration time. A zero
// DuplicateCheck reads up to five pages of 100 open cards.
func NewFlow(connection trello.Connection, duplicateCheck DuplicateCheck) *Flow {
	if duplicateCheck.PageSize < 1 || duplicateCheck.PageSize > DefaultOpenCardPageSize {
		duplicateCheck.PageSize = DefaultOpenCardPageSize
	}
	if duplicateCheck.MaximumPages < 1 {
		duplicateCheck.MaximumPages = DefaultMaximumOpenCardPages
	}
	return &Flow{connection: connection, duplicateCheck: duplicateCheck}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Trello connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordApprovedRequest{pageSize: flow.duplicateCheck.PageSize}),
		dex.DefineStep(trello.NewListCardsStep(trello.ListCardsStepConfig[OpenCardPage]{
			StepType: findOpenRequestCardsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "trello", GroupLabel: "Trello",
				Explanation: "List one page of the board's open cards, newest first, to find one that already carries the request ID.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListCardsInput,
			Listed: sdkgo.GoTo(evaluateOpenRequestCards{duplicateCheck: flow.duplicateCheck}),
		})),
		dex.DefineStep(evaluateOpenRequestCards{duplicateCheck: flow.duplicateCheck}),
		dex.DefineStep(trello.NewCreateCardStep(trello.CreateCardStepConfig[CardRequest]{
			StepType: createRequestCardStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "trello", GroupLabel: "Trello",
				Explanation: "Create the card in the approved list once; an unknown outcome is reconciled, never created again automatically.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateCardInput,
			Created:          sdkgo.GoTo(recordCreatedCard{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedCard{}),
			Uncertain:        sdkgo.GoTo(recordUncertainCard{}),
		})),
		dex.DefineStep(recordCreatedCard{}),
		dex.DefineStep(recordRejectedCard{}),
		dex.DefineStep(recordUncertainCard{}),
		dex.DefineStep(trello.NewGetCardStep(trello.GetCardStepConfig[CardReference]{
			StepType: readReportedCardStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "trello", GroupLabel: "Trello",
				Explanation: "Read the card an operator reported to confirm its board and request ID.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetCardInput,
			Found:    sdkgo.GoTo(verifyReportedCard{}),
			NotFound: sdkgo.GoTo(recordMissingReportedCard{}),
		})),
		dex.DefineStep(verifyReportedCard{}),
		dex.DefineStep(recordMissingReportedCard{}),
		dex.DefineStep(trello.NewUpdateCardStep(trello.UpdateCardStepConfig[CardMove]{
			StepType: moveRequestCardStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "trello", GroupLabel: "Trello",
				Explanation: "Reopen the card, move it to the approved list, set its due date, and add the request labels; a repeated update leaves the same card.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateCardInput,
			Updated:          sdkgo.GoTo(recordMovedCard{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedMove{}),
		})),
		dex.DefineStep(recordMovedCard{}),
		dex.DefineStep(recordRejectedMove{}),
		dex.DefineStep(trello.NewAddCommentStep(trello.AddCommentStepConfig[CommentRequest]{
			StepType: commentOnRequestCardStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "trello", GroupLabel: "Trello",
				Explanation: "Add the approval comment once; an unknown outcome is recorded, never re-sent.",
			},
			Connection: flow.connection, MapToOperationInput: MapToAddCommentInput,
			Added:     sdkgo.GoTo(recordApprovalComment{}),
			Uncertain: sdkgo.GoTo(recordApprovalComment{}),
		})),
		dex.DefineStep(recordApprovalComment{}),
	}
}

// GetRPCs returns the reconciliation Actions, the record read RPC, and the Dex Web views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	reconciliationLocks := []dex.AttributeLock{dex.LockAttribute(requestCardPhaseAttribute), dex.LockAttribute(requestCardAttribute)}
	return []dex.RPCDef{
		dex.DefineRPC(flow.ConfirmCreatedCard, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Confirm created card",
				dex.WhenAttributeMatches(requestCardPhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsReconciliation)),
				dex.ActionRequiresPermission(ReconcileRequestCardPermission),
			),
			LockAttributes: reconciliationLocks,
		}),
		dex.DefineRPC(flow.ApproveCardCreateRetry, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Create card again",
				dex.WhenAttributeMatches(requestCardPhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsReconciliation)),
				dex.ActionRequiresPermission(ReconcileRequestCardPermission),
			),
			LockAttributes: reconciliationLocks,
		}),
		dex.DefineRPC(flow.GetRequestCard, nil),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the phase and record Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{requestCardPhaseAttribute, requestCardAttribute}}
}

// MapToListCardsInput lists one page of the board's open cards, newest first.
func MapToListCardsInput(page OpenCardPage) trello.ListCardsInput {
	return trello.ListCardsInput{BoardID: page.BoardID, Status: trello.CardStatusOpen, PageSize: page.PageSize, Before: page.Before}
}

// MapToCreateCardInput maps the recorded request to one new card in the approved list.
func MapToCreateCardInput(request CardRequest) trello.CreateCardInput {
	return trello.CreateCardInput{
		ListID: request.ListID, Name: request.CardName, Description: request.Description, Due: request.Due,
		LabelIDs: request.LabelIDs, MemberIDs: request.MemberIDs, Position: "top",
	}
}

// MapToGetCardInput reads the card an operator reported.
func MapToGetCardInput(reference CardReference) trello.GetCardInput {
	return trello.GetCardInput{CardID: reference.CardID}
}

// MapToUpdateCardInput reopens the card, moves it to the approved list, sets a requested due date, and adds
// the request labels without removing the card's other labels.
func MapToUpdateCardInput(move CardMove) trello.UpdateCardInput {
	isClosed := false
	return trello.UpdateCardInput{CardID: move.CardID, ListID: move.ListID, IsClosed: &isClosed, Due: move.Due, AddLabelIDs: move.LabelIDs}
}

// MapToAddCommentInput maps the approval comment to one Trello comment.
func MapToAddCommentInput(request CommentRequest) trello.AddCommentInput {
	return trello.AddCommentInput{CardID: request.CardID, Text: request.Text}
}

// CardNamePrefix is the bracketed request ID every request card name starts with, such as [REQ-1042].
func CardNamePrefix(requestID string) string { return "[" + requestID + "]" }

// FindRequestCard returns the first open card whose name starts with the request's bracketed ID.
// [REQ-10420] and a name that only mentions REQ-1042 elsewhere are not the same request.
func FindRequestCard(cards []trello.Card, requestID string) (trello.Card, bool) {
	prefix := CardNamePrefix(requestID)
	for _, card := range cards {
		if !card.IsClosed && strings.HasPrefix(strings.TrimSpace(card.Name), prefix) {
			return card, true
		}
	}
	return trello.Card{}, false
}

// ConfirmCreatedCard adopts a card an operator found on the board after an uncertain create. The Flow reads
// it with getCard and adopts it only when it is on the board and its name starts with the request ID.
//
// dex:input field-name:cardId value-type:string source:user required:true description:"ID of the card found on the board, 24 hexadecimal characters"
func (*Flow) ConfirmCreatedCard(ctx dex.Context, input ConfirmCreatedCardInput) (*dex.RPCResult[dex.None], error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.Phase != PhaseNeedsReconciliation {
		return &dex.RPCResult[dex.None]{}, nil
	}
	cardID := strings.TrimSpace(input.CardID)
	if !isTrelloID(cardID) {
		return nil, errReportedCardIDInvalid
	}
	record.Phase = PhaseVerifyingReportedCard
	record.ReconciliationNote = ""
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[CardReference](readReportedCardStepType), CardReference{CardID: cardID})},
	}, nil
}

// ApproveCardCreateRetry creates the card again after an operator confirmed the board has no such card. The
// retry is a new Step execution with a new connector call ID.
func (*Flow) ApproveCardCreateRetry(ctx dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.Phase != PhaseNeedsReconciliation {
		return &dex.RPCResult[dex.None]{}, nil
	}
	record.Phase = PhaseCreating
	record.CreateAttempts++
	record.UncertainCreate = nil
	record.ReconciliationNote = ""
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[CardRequest](createRequestCardStepType), record.Request)},
	}, nil
}

// GetRequestCard returns the current record.
func (*Flow) GetRequestCard(ctx dex.Context, _ dex.None) (*dex.RPCResult[RequestCard], error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[RequestCard]{Output: record}, nil
}

// GetDexSummary returns the phase and record for Dex Web lists.
//
// dex:field attribute-key:trello-request-card-phase value-type:string editable:false description:"Request card phase"
// dex:field attribute-key:trello-request-card value-type:json editable:false description:"Request, card ID, move, and approval comment"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, requestCardPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, requestCardAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"trello-request-card-phase": phase,
		"trello-request-card":       record,
	}}, nil
}

// GetDexDisplay returns the phase and record for the Dex Web run view.
//
// dex:field attribute-key:trello-request-card-phase value-type:string editable:false description:"Request card phase" ui-slot:status
// dex:field attribute-key:trello-request-card value-type:json editable:false description:"Request, card read back after a move, approval comment, and any uncertain create awaiting reconciliation"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, requestCardPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, requestCardAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"trello-request-card-phase": phase,
		"trello-request-card":       record,
	}}, nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Validate the approved request and record it before calling Trello."
type recordApprovedRequest struct {
	dex.StepDefaults
	pageSize int
}

func (recordApprovedRequest) GetStepType() string { return recordApprovedRequestStepType }

func (recordApprovedRequest) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestCardPhaseAttribute), dex.LockAttribute(requestCardAttribute)}}
}

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordApprovedRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordApprovedRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildCardRequest(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	record := RequestCard{Request: request, Phase: PhaseSearching}
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[OpenCardPage](findOpenRequestCardsStepType), OpenCardPage{BoardID: request.BoardID, PageSize: step.pageSize}), nil
}

// BuildCardRequest validates Start Flow input.
func BuildCardRequest(input Input) (CardRequest, error) {
	request := CardRequest{
		RequestID: strings.TrimSpace(input.RequestID), Description: input.Details,
		BoardID: strings.TrimSpace(input.BoardID), ListID: strings.TrimSpace(input.ListID), Due: input.DueAt,
	}
	title, approvedBy := strings.TrimSpace(input.Title), strings.TrimSpace(input.ApprovedBy)
	switch {
	case !isRequestID(request.RequestID):
		return CardRequest{}, errors.New("requestId must be 1 to 64 letters, digits, dashes, or underscores, such as REQ-1042")
	case title == "" || strings.ContainsAny(title, "\r\n"):
		return CardRequest{}, errors.New("title is required and must be one line")
	case approvedBy == "" || strings.ContainsAny(approvedBy, "\r\n"):
		return CardRequest{}, errors.New("approvedBy is required and must be one line")
	case !isTrelloID(request.BoardID):
		return CardRequest{}, errors.New("boardId must be the board's 24-character Trello ID")
	case !isTrelloID(request.ListID):
		return CardRequest{}, errors.New("listId must be the approved list's 24-character Trello ID")
	}
	for _, values := range [][]string{input.LabelIDs, input.MemberIDs} {
		for _, value := range values {
			if !isTrelloID(strings.TrimSpace(value)) {
				return CardRequest{}, errors.New("labelIds and memberIds must hold 24-character Trello IDs")
			}
		}
	}
	request.LabelIDs, request.MemberIDs = trimAll(input.LabelIDs), trimAll(input.MemberIDs)
	request.CardName = CardNamePrefix(request.RequestID) + " " + title
	request.Comment = "Approved by " + approvedBy + "."
	if note := strings.TrimSpace(input.ApprovalNote); note != "" {
		request.Comment += "\n\n" + note
	}
	return request, nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Reuse an open card that carries the request ID, read the next page, or create the card."
type evaluateOpenRequestCards struct {
	dex.StepDefaultsNoWaitFor[trello.ListCardsResult]
	duplicateCheck DuplicateCheck
}

func (evaluateOpenRequestCards) GetStepType() string { return evaluateOpenRequestCardsStepType }

func (evaluateOpenRequestCards) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestCardPhaseAttribute), dex.LockAttribute(requestCardAttribute)}}
}

func (step evaluateOpenRequestCards) Execute(ctx dex.Context, result trello.ListCardsResult) (*dex.StepDecision, error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.OpenCardPagesRead++
	if card, isFound := FindRequestCard(result.Value.Cards, record.Request.RequestID); isFound {
		record.Phase, record.IsExistingCardReused, record.CardID, record.CardURL = PhaseMoving, true, card.ID, card.URL
		if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := requestCardAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[CardMove](moveRequestCardStepType), newCardMove(record)), nil
	}
	if result.Value.NextBefore != "" {
		if record.OpenCardPagesRead >= step.duplicateCheck.MaximumPages {
			record.Phase = PhaseDuplicateCheckIncomplete
			if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
				return nil, err
			}
			if err := requestCardAttribute.Set(ctx, record); err != nil {
				return nil, err
			}
			return dex.GracefulComplete(record), nil
		}
		if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := requestCardAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[OpenCardPage](findOpenRequestCardsStepType), OpenCardPage{
			BoardID: record.Request.BoardID, PageSize: step.duplicateCheck.PageSize, Before: result.Value.NextBefore,
		}), nil
	}
	record.Phase = PhaseCreating
	record.CreateAttempts = 1
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CardRequest](createRequestCardStepType), record.Request), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Record the created card and add the approval comment."
type recordCreatedCard struct {
	dex.StepDefaultsNoWaitFor[trello.CreateCardResult]
}

func (recordCreatedCard) GetStepType() string { return recordCreatedCardStepType }

func (recordCreatedCard) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestCardPhaseAttribute), dex.LockAttribute(requestCardAttribute)}}
}

func (recordCreatedCard) Execute(ctx dex.Context, result trello.CreateCardResult) (*dex.StepDecision, error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.CardID, record.CardURL = PhaseCommenting, result.Value.CardID, result.Value.URL
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CommentRequest](commentOnRequestCardStepType), CommentRequest{CardID: record.CardID, Text: record.Request.Comment}), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Complete as rejected after Trello conclusively refused the create."
type recordRejectedCard struct {
	dex.StepDefaultsNoWaitFor[trello.CreateCardResult]
}

func (recordRejectedCard) GetStepType() string { return recordRejectedCardStepType }

func (recordRejectedCard) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestCardPhaseAttribute), dex.LockAttribute(requestCardAttribute)}}
}

func (recordRejectedCard) Execute(ctx dex.Context, _ trello.CreateCardResult) (*dex.StepDecision, error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseRejected
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Park an unknown create for an operator instead of creating the card again."
type recordUncertainCard struct {
	dex.StepDefaultsNoWaitFor[trello.CreateCardResult]
}

func (recordUncertainCard) GetStepType() string { return recordUncertainCardStepType }

func (recordUncertainCard) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestCardPhaseAttribute), dex.LockAttribute(requestCardAttribute)}}
}

func (recordUncertainCard) Execute(ctx dex.Context, result trello.CreateCardResult) (*dex.StepDecision, error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseNeedsReconciliation
	record.UncertainCreate = &UncertainCreate{CallID: string(result.Receipt.CallID), ObservedAt: result.Receipt.ObservedAt}
	if result.Failure != nil {
		record.UncertainCreate.FailureKind, record.UncertainCreate.FailureMessage = result.Failure.Kind, result.Failure.Message
	}
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Adopt the reported card when it is on the board and carries the request ID, or return it to the operator."
type verifyReportedCard struct {
	dex.StepDefaultsNoWaitFor[trello.GetCardResult]
}

func (verifyReportedCard) GetStepType() string { return verifyReportedCardStepType }

func (verifyReportedCard) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestCardPhaseAttribute), dex.LockAttribute(requestCardAttribute)}}
}

func (verifyReportedCard) Execute(ctx dex.Context, result trello.GetCardResult) (*dex.StepDecision, error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	card := result.Value
	if !strings.EqualFold(card.BoardID, record.Request.BoardID) || !strings.HasPrefix(strings.TrimSpace(card.Name), CardNamePrefix(record.Request.RequestID)) {
		record.Phase, record.ReconciliationNote = PhaseNeedsReconciliation, NoteReportedCardMismatch
		if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := requestCardAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.DeadEnd(), nil
	}
	record.Phase, record.CardID, record.CardURL, record.UncertainCreate = PhaseMoving, card.ID, card.URL, nil
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CardMove](moveRequestCardStepType), newCardMove(record)), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Return a reported card ID that Trello cannot find to the operator."
type recordMissingReportedCard struct {
	dex.StepDefaultsNoWaitFor[trello.GetCardResult]
}

func (recordMissingReportedCard) GetStepType() string { return recordMissingReportedCardStepType }

func (recordMissingReportedCard) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestCardPhaseAttribute), dex.LockAttribute(requestCardAttribute)}}
}

func (recordMissingReportedCard) Execute(ctx dex.Context, _ trello.GetCardResult) (*dex.StepDecision, error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.ReconciliationNote = PhaseNeedsReconciliation, NoteReportedCardMissing
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Record the moved card read back and add the approval comment."
type recordMovedCard struct {
	dex.StepDefaultsNoWaitFor[trello.UpdateCardResult]
}

func (recordMovedCard) GetStepType() string { return recordMovedCardStepType }

func (recordMovedCard) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestCardPhaseAttribute), dex.LockAttribute(requestCardAttribute)}}
}

func (recordMovedCard) Execute(ctx dex.Context, result trello.UpdateCardResult) (*dex.StepDecision, error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.Card = PhaseCommenting, result.Value.Card
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CommentRequest](commentOnRequestCardStepType), CommentRequest{CardID: record.CardID, Text: record.Request.Comment}), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Complete with the existing card when Trello rejected its move, due date, or labels."
type recordRejectedMove struct {
	dex.StepDefaultsNoWaitFor[trello.UpdateCardResult]
}

func (recordRejectedMove) GetStepType() string { return recordRejectedMoveStepType }

func (recordRejectedMove) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestCardPhaseAttribute), dex.LockAttribute(requestCardAttribute)}}
}

func (recordRejectedMove) Execute(ctx dex.Context, _ trello.UpdateCardResult) (*dex.StepDecision, error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseMoveRejected
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Record the approval comment ID, or that its outcome is unknown, and complete without re-sending."
type recordApprovalComment struct {
	dex.StepDefaultsNoWaitFor[trello.AddCommentResult]
}

func (recordApprovalComment) GetStepType() string { return recordApprovalCommentStepType }

func (recordApprovalComment) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestCardPhaseAttribute), dex.LockAttribute(requestCardAttribute)}}
}

func (recordApprovalComment) Execute(ctx dex.Context, result trello.AddCommentResult) (*dex.StepDecision, error) {
	record, err := requestCardAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseReady
	record.CommentID = result.Value.CommentID
	record.IsCommentOutcomeUnknown = result.Branch == trello.AddCommentBranchUncertain
	if err := requestCardPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestCardAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

func newCardMove(record RequestCard) CardMove {
	return CardMove{CardID: record.CardID, ListID: record.Request.ListID, Due: record.Request.Due, LabelIDs: record.Request.LabelIDs}
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

func trimAll(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		trimmed = append(trimmed, strings.TrimSpace(value))
	}
	return trimmed
}

func isTrelloID(value string) bool {
	if len(value) != 24 {
		return false
	}
	for _, character := range value {
		isHexadecimal := (character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')
		if !isHexadecimal {
			return false
		}
	}
	return true
}

func isRequestID(value string) bool {
	if value == "" || len(value) > maximumRequestIDSize {
		return false
	}
	for _, character := range value {
		isLetterOrDigit := (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')
		if !isLetterOrDigit && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[ConfirmCreatedCardInput, dex.None] = (*Flow)(nil).ConfirmCreatedCard
var _ dex.RPC[dex.None, dex.None] = (*Flow)(nil).ApproveCardCreateRetry
var _ dex.RPC[dex.None, RequestCard] = (*Flow)(nil).GetRequestCard
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
