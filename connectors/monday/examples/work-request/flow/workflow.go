// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package workrequest demonstrates every monday.com operation in one Flow started from Dex
// Web Start Flow: find the board's open item for a recurring work request, or create one, set
// its status and due date, and add one update describing what the Flow did.
package workrequest

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/monday"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "MondayWorkRequest"
	// ConnectionName is the static Dex Web connection for monday.com.
	ConnectionName = "monday-workspace"
	// CompletedStatusLabel is the status label of a finished item, which the Flow never reschedules.
	CompletedStatusLabel = "Done"

	recordWorkRequestStepType     = "RecordWorkRequest"
	findExistingItemStepType      = "FindExistingItem"
	chooseExistingItemStepType    = "ChooseExistingItem"
	readExistingItemStepType      = "ReadExistingItem"
	confirmExistingItemStepType   = "ConfirmExistingItem"
	scheduleExistingItemStepType  = "ScheduleExistingItem"
	recordScheduledItemStepType   = "RecordScheduledItem"
	createWorkItemStepType        = "CreateWorkItem"
	recordCreatedItemStepType     = "RecordCreatedItem"
	recordUncertainItemStepType   = "RecordUncertainItem"
	addWorkUpdateStepType         = "AddWorkUpdate"
	completeWorkRequestStepType   = "CompleteWorkRequest"
	recordUncertainUpdateStepType = "RecordUncertainUpdate"

	// MaxSearchPages bounds how many pages of same-name items the Flow reads before creating one.
	MaxSearchPages  = 5
	searchPageLimit = 25

	createWorkItemReviewReason = "createItem"
	addWorkUpdateReviewReason  = "addUpdate"
)

var (
	workRequestAttribute  = dex.DefineAttribute[WorkRequest]("monday-work-request")
	itemSearchAttribute   = dex.DefineAttribute[ItemSearchProgress]("monday-item-search")
	workOutcomeAttribute  = dex.DefineAttribute[WorkRequestOutcome]("monday-work-outcome")
	errInvalidWorkRequest = errors.New("invalid work request")
)

// Input is the work request entered in Dex Web Start Flow.
type Input struct {
	// BoardID is the numeric board ID, the number after /boards/ in the board's URL.
	BoardID string `json:"boardId"`
	// GroupID places a newly created item in this group, such as topics; blank uses the board's first group.
	GroupID string `json:"groupId,omitempty"`
	// ItemName is the work item's exact name, such as Monthly Fire Drill Checklist - February.
	ItemName string `json:"itemName"`
	// StatusColumnID is the ID of the board's status column, such as status.
	StatusColumnID string `json:"statusColumnId"`
	// StatusLabel is an existing label of that status column, such as Working on it.
	StatusLabel string `json:"statusLabel"`
	// DueDateColumnID is the ID of the board's date column, such as date4.
	DueDateColumnID string `json:"dueDateColumnId"`
	// DueDate is a YYYY-MM-DD date such as 2026-02-18.
	DueDate string `json:"dueDate"`
	// UpdateText is plain text the Flow adds to the item as a monday.com update.
	UpdateText string `json:"updateText"`
}

// WorkRequest is the validated request every later Step reads.
type WorkRequest struct {
	// BoardID is the numeric board ID.
	BoardID string `json:"boardId"`
	// GroupID places a new item in this group, or blank.
	GroupID string `json:"groupId,omitempty"`
	// ItemName is the exact item name.
	ItemName string `json:"itemName"`
	// StatusColumnID is the status column ID.
	StatusColumnID string `json:"statusColumnId"`
	// StatusLabel is the status label to set.
	StatusLabel string `json:"statusLabel"`
	// DueDateColumnID is the date column ID.
	DueDateColumnID string `json:"dueDateColumnId"`
	// DueDate is the YYYY-MM-DD due date.
	DueDate string `json:"dueDate"`
	// UpdateText is the update's plain text.
	UpdateText string `json:"updateText"`
}

// ItemSearch is one page of the search for an item with the request's name.
type ItemSearch struct {
	// BoardID is the numeric board ID.
	BoardID string `json:"boardId"`
	// ItemName is the exact item name.
	ItemName string `json:"itemName"`
	// ColumnIDs are the status and due date columns each listed item returns.
	ColumnIDs []string `json:"columnIds"`
	// Cursor continues the previous page; blank reads the first page.
	Cursor string `json:"cursor,omitempty"`
}

// ItemSearchProgress counts the search pages read, so the search stays bounded.
type ItemSearchProgress struct {
	// PagesRead is how many pages of same-name items the Flow has read.
	PagesRead int `json:"pagesRead"`
}

// ItemReference identifies an item the Flow reads or schedules.
type ItemReference struct {
	// BoardID is the numeric board ID.
	BoardID string `json:"boardId"`
	// ItemID is the numeric item ID.
	ItemID string `json:"itemId"`
}

// ItemSchedule is the confirmed item and the request whose status and due date it receives.
type ItemSchedule struct {
	// BoardID is the numeric board ID.
	BoardID string `json:"boardId"`
	// ItemID is the numeric item ID.
	ItemID string `json:"itemId"`
	// Request names the status and due date columns and values.
	Request WorkRequest `json:"request"`
}

// WorkUpdate is the update to add to the item.
type WorkUpdate struct {
	// ItemID is the numeric item ID.
	ItemID string `json:"itemId"`
	// Body is the update's plain text.
	Body string `json:"body"`
}

// WorkAction is what the Flow did with the item.
type WorkAction string

const (
	// WorkItemCreated means the board had no open item with the name, so one was created.
	WorkItemCreated WorkAction = "created"
	// WorkItemScheduled means the board's open item with the name was given the status and due date.
	WorkItemScheduled WorkAction = "scheduled"
	// WorkItemCreationUncertain means monday.com accepted the create but its answer was unusable.
	WorkItemCreationUncertain WorkAction = "creationUncertain"
)

// WorkRequestOutcome is the Flow result and the value of its outcome Attribute.
type WorkRequestOutcome struct {
	// Action is what the Flow did with the item.
	Action WorkAction `json:"action"`
	// ItemID is the created or scheduled item.
	ItemID string `json:"itemId,omitempty"`
	// ItemURL is the item's monday.com page.
	ItemURL string `json:"itemUrl,omitempty"`
	// GroupID is the group that holds the item.
	GroupID string `json:"groupId,omitempty"`
	// SkippedItemID is a same-name search match the Flow did not schedule, because when read it was
	// gone, inactive, completed, or no longer had the name.
	SkippedItemID string `json:"skippedItemId,omitempty"`
	// WasCreateReplayed reports that monday.com answered the create from its idempotency cache,
	// because an earlier attempt of the same Step had already created the item.
	WasCreateReplayed bool `json:"createReplayed,omitempty"`
	// UpdateID is the update the Flow added.
	UpdateID string `json:"updateId,omitempty"`
	// NeedsReview reports that monday.com accepted a write but its answer was unusable, so a person
	// must check the board; the Flow never sends it again.
	NeedsReview bool `json:"needsReview,omitempty"`
	// ReviewReason names the write: createItem or addUpdate.
	ReviewReason string `json:"reviewReason,omitempty"`
	// ReviewDetail is the connector's credential-free explanation.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// Flow handles one recurring work request on a monday.com board.
type Flow struct {
	dex.FlowDefaults
	connection monday.Connection
}

// NewFlow binds the monday.com Connection at registration time.
func NewFlow(connection monday.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and monday.com connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordWorkRequest{}),
		dex.DefineStep(monday.NewListItemsStep(monday.ListItemsStepConfig[ItemSearch]{
			StepType: findExistingItemStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "monday", GroupLabel: "monday.com",
				Explanation: "List one page of the board's active items whose name is exactly the request's name.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListItemsInput,
			Listed: sdkgo.GoTo(chooseExistingItem{}),
		})),
		dex.DefineStep(chooseExistingItem{}),
		dex.DefineStep(monday.NewGetItemStep(monday.GetItemStepConfig[ItemReference]{
			StepType: readExistingItemStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "monday", GroupLabel: "monday.com",
				Explanation: "Read the candidate item with its status and due date before writing to it.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetItemInput,
			Found:    sdkgo.GoTo(confirmExistingItem{}),
			NotFound: sdkgo.GoTo(confirmExistingItem{}),
		})),
		dex.DefineStep(confirmExistingItem{}),
		dex.DefineStep(monday.NewUpdateItemColumnValuesStep(monday.UpdateItemColumnValuesStepConfig[ItemSchedule]{
			StepType: scheduleExistingItemStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "monday", GroupLabel: "monday.com",
				Explanation: "Set the item's status and due date with absolute values, so a repeated attempt changes nothing.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateItemColumnValuesInput,
			Updated: sdkgo.GoTo(recordScheduledItem{}),
		})),
		dex.DefineStep(recordScheduledItem{}),
		dex.DefineStep(monday.NewCreateItemStep(monday.CreateItemStepConfig[WorkRequest]{
			StepType: createWorkItemStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "monday", GroupLabel: "monday.com",
				Explanation: "Create the item with its status and due date under the Step's idempotency key.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateItemInput,
			Created:   sdkgo.GoTo(recordCreatedItem{}),
			Uncertain: sdkgo.GoTo(recordUncertainItem{}),
		})),
		dex.DefineStep(recordCreatedItem{}),
		dex.DefineStep(recordUncertainItem{}),
		dex.DefineStep(monday.NewAddUpdateStep(monday.AddUpdateStepConfig[WorkUpdate]{
			StepType: addWorkUpdateStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "monday", GroupLabel: "monday.com",
				Explanation: "Add one update describing what the Flow did, under the Step's idempotency key.",
			},
			Connection: flow.connection, MapToOperationInput: MapToAddUpdateInput,
			Added:     sdkgo.GoTo(completeWorkRequest{}),
			Uncertain: sdkgo.GoTo(recordUncertainUpdate{}),
		})),
		dex.DefineStep(completeWorkRequest{}),
		dex.DefineStep(recordUncertainUpdate{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request, search progress, and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{workRequestAttribute, itemSearchAttribute, workOutcomeAttribute}}
}

// GetDexSummary returns the work request and its outcome.
//
// dex:field attribute-key:monday-work-request value-type:json editable:false description:"Work request"
// dex:field attribute-key:monday-work-outcome value-type:json editable:false description:"monday.com outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := workInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"monday-work-request": request, "monday-work-outcome": outcome}}, nil
}

// GetDexDisplay returns the work request and the item and update monday.com returned.
//
// dex:field attribute-key:monday-work-request value-type:json editable:false description:"Board, item name, status, due date, and update text"
// dex:field attribute-key:monday-work-outcome value-type:json editable:false description:"Action, item, update, and review state"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := workInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"monday-work-request": request, "monday-work-outcome": outcome}}, nil
}

// MapToListItemsInput lists items named exactly like the request, with the status and due date columns.
func MapToListItemsInput(search ItemSearch) monday.ListItemsInput {
	if search.Cursor != "" {
		return monday.ListItemsInput{BoardID: search.BoardID, Cursor: search.Cursor, Limit: searchPageLimit, ColumnIDs: search.ColumnIDs}
	}
	return monday.ListItemsInput{
		BoardID: search.BoardID, Limit: searchPageLimit, ColumnIDs: search.ColumnIDs,
		Filter: monday.ItemFilter{Rules: []monday.ItemFilterRule{{ColumnID: "name", Operator: monday.ItemFilterAnyOf, Values: []string{search.ItemName}}}},
	}
}

// MapToGetItemInput reads the candidate item, including an archived or deleted one, so its state decides.
func MapToGetItemInput(reference ItemReference) monday.GetItemInput {
	return monday.GetItemInput{ItemID: reference.ItemID, IncludesInactive: true}
}

// MapToCreateItemInput creates the item with the request's status and due date.
func MapToCreateItemInput(request WorkRequest) monday.CreateItemInput {
	return monday.CreateItemInput{
		BoardID: request.BoardID, GroupID: request.GroupID, ItemName: request.ItemName, ColumnValues: BuildScheduleColumnValues(request),
	}
}

// MapToAddUpdateInput adds the update to the item.
func MapToAddUpdateInput(update WorkUpdate) monday.AddUpdateInput {
	return monday.AddUpdateInput{ItemID: update.ItemID, Body: update.Body}
}

// MapToUpdateItemColumnValuesInput sets the confirmed item's status and due date.
func MapToUpdateItemColumnValuesInput(schedule ItemSchedule) monday.UpdateItemColumnValuesInput {
	return monday.UpdateItemColumnValuesInput{
		BoardID: schedule.BoardID, ItemID: schedule.ItemID, ColumnValues: BuildScheduleColumnValues(schedule.Request),
	}
}

// BuildScheduleColumnValues sets the status label and due date the request names.
func BuildScheduleColumnValues(request WorkRequest) map[string]monday.ColumnValue {
	return map[string]monday.ColumnValue{
		request.StatusColumnID:  monday.StatusLabelValue(request.StatusLabel),
		request.DueDateColumnID: monday.DateValue(request.DueDate),
	}
}

// ChooseOpenItem returns the first listed item named exactly like the request whose status is not
// CompletedStatusLabel, or false when the page has none. The name filter compares monday.com's text,
// so the Flow checks the exact name again.
func ChooseOpenItem(items []monday.Item, request WorkRequest) (monday.Item, bool) {
	for _, item := range items {
		if IsOpenWorkItem(item, request) {
			return item, true
		}
	}
	return monday.Item{}, false
}

// IsOpenWorkItem reports whether the item, as read, is an active item on the request's board with the
// exact name and a status other than CompletedStatusLabel.
func IsOpenWorkItem(item monday.Item, request WorkRequest) bool {
	if item.Name != request.ItemName || (item.State != "" && item.State != monday.ItemStateActive) {
		return false
	}
	if item.BoardID != "" && item.BoardID != request.BoardID {
		return false
	}
	status, hasStatus := item.ColumnValueByID(request.StatusColumnID)
	return !hasStatus || !strings.EqualFold(strings.TrimSpace(status.Text), CompletedStatusLabel)
}

// BuildUpdateBody is the update the Flow adds after creating or scheduling the item.
func BuildUpdateBody(request WorkRequest, action WorkAction) string {
	verb := "created"
	if action == WorkItemScheduled {
		verb = "scheduled"
	}
	return "Dex " + verb + " this item: status " + request.StatusLabel + ", due " + request.DueDate + ".\n\n" + request.UpdateText
}

func workInspection(ctx dex.Context) (WorkRequest, WorkRequestOutcome, error) {
	request, err := optionalAttribute(ctx, workRequestAttribute)
	if err != nil {
		return WorkRequest{}, WorkRequestOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, workOutcomeAttribute)
	if err != nil {
		return WorkRequest{}, WorkRequestOutcome{}, err
	}
	return request, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	if isAttributeNotFound(err) {
		var zero T
		return zero, nil
	}
	return value, err
}

func failureMessage(failure *sdkgo.Failure) string {
	if failure == nil {
		return ""
	}
	return failure.Message
}

func isAttributeNotFound(err error) bool {
	var missingAttribute *dex.AttributeNotFoundError
	return errors.As(err, &missingAttribute)
}

// dex:group group-id:request group-label:"Work request"
// dex:explanation text:"Validate the work request and record it before calling monday.com."
type recordWorkRequest struct {
	dex.StepDefaults
}

func (recordWorkRequest) GetStepType() string { return recordWorkRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordWorkRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordWorkRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildWorkRequest(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := workRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	if err := itemSearchAttribute.Set(ctx, ItemSearchProgress{PagesRead: 1}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ItemSearch](findExistingItemStepType), newItemSearch(request, "")), nil
}

// BuildWorkRequest validates Start Flow input, so no connector Step receives an unusable request.
func BuildWorkRequest(input Input) (WorkRequest, error) {
	request := WorkRequest{
		BoardID: strings.TrimSpace(input.BoardID), GroupID: strings.TrimSpace(input.GroupID), ItemName: strings.TrimSpace(input.ItemName),
		StatusColumnID: strings.TrimSpace(input.StatusColumnID), StatusLabel: strings.TrimSpace(input.StatusLabel),
		DueDateColumnID: strings.TrimSpace(input.DueDateColumnID), DueDate: strings.TrimSpace(input.DueDate),
		UpdateText: strings.TrimSpace(input.UpdateText),
	}
	switch {
	case request.BoardID == "" || strings.Trim(request.BoardID, "0123456789") != "":
		return WorkRequest{}, fmt.Errorf("%w: boardId must be the numeric board ID from the board's URL", errInvalidWorkRequest)
	case request.ItemName == "" || request.StatusLabel == "" || request.UpdateText == "":
		return WorkRequest{}, fmt.Errorf("%w: itemName, statusLabel, and updateText are required", errInvalidWorkRequest)
	case request.StatusColumnID == "" || request.DueDateColumnID == "" || request.StatusColumnID == request.DueDateColumnID:
		return WorkRequest{}, fmt.Errorf("%w: statusColumnId and dueDateColumnId name two different columns", errInvalidWorkRequest)
	}
	if _, err := time.Parse("2006-01-02", request.DueDate); err != nil {
		return WorkRequest{}, fmt.Errorf("%w: dueDate must be a YYYY-MM-DD date such as 2026-02-18", errInvalidWorkRequest)
	}
	return request, nil
}

func newItemSearch(request WorkRequest, cursor string) ItemSearch {
	return ItemSearch{
		BoardID: request.BoardID, ItemName: request.ItemName,
		ColumnIDs: []string{request.StatusColumnID, request.DueDateColumnID}, Cursor: cursor,
	}
}

// dex:group group-id:request group-label:"Work request"
// dex:explanation text:"Read the first open same-name item, search the next page when this one has none, or create the item."
type chooseExistingItem struct {
	dex.StepDefaultsNoWaitFor[monday.ListItemsResult]
}

func (chooseExistingItem) GetStepType() string { return chooseExistingItemStepType }

func (chooseExistingItem) Execute(ctx dex.Context, result monday.ListItemsResult) (*dex.StepDecision, error) {
	request, err := workRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if item, isFound := ChooseOpenItem(result.Value.Items, request); isFound {
		return dex.GoTo(sdkgo.StepRef[ItemReference](readExistingItemStepType), ItemReference{BoardID: request.BoardID, ItemID: item.ID}), nil
	}
	progress, err := itemSearchAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if result.Value.NextCursor != "" && progress.PagesRead < MaxSearchPages {
		if err := itemSearchAttribute.Set(ctx, ItemSearchProgress{PagesRead: progress.PagesRead + 1}); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[ItemSearch](findExistingItemStepType), newItemSearch(request, result.Value.NextCursor)), nil
	}
	return dex.GoTo(sdkgo.StepRef[WorkRequest](createWorkItemStepType), request), nil
}

// dex:group group-id:request group-label:"Work request"
// dex:explanation text:"Schedule the item only when, as just read, it is still the board's open item with the name; otherwise create one."
type confirmExistingItem struct {
	dex.StepDefaultsNoWaitFor[monday.GetItemResult]
}

func (confirmExistingItem) GetStepType() string { return confirmExistingItemStepType }

func (confirmExistingItem) Execute(ctx dex.Context, result monday.GetItemResult) (*dex.StepDecision, error) {
	request, err := workRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if result.Branch == monday.GetItemBranchFound && IsOpenWorkItem(result.Value, request) {
		return dex.GoTo(sdkgo.StepRef[ItemSchedule](scheduleExistingItemStepType),
			ItemSchedule{BoardID: request.BoardID, ItemID: result.Value.ID, Request: request}), nil
	}
	if err := workOutcomeAttribute.Set(ctx, WorkRequestOutcome{SkippedItemID: result.Receipt.ProviderObjectID}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[WorkRequest](createWorkItemStepType), request), nil
}

// dex:group group-id:request group-label:"Work request"
// dex:explanation text:"Record the scheduled item and prepare its update."
type recordScheduledItem struct {
	dex.StepDefaultsNoWaitFor[monday.UpdateItemColumnValuesResult]
}

func (recordScheduledItem) GetStepType() string { return recordScheduledItemStepType }

func (recordScheduledItem) Execute(ctx dex.Context, result monday.UpdateItemColumnValuesResult) (*dex.StepDecision, error) {
	request, err := workRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := WorkRequestOutcome{Action: WorkItemScheduled, ItemID: result.Value.ItemID}
	if item := result.Value.Item; item != nil {
		outcome.ItemURL = item.URL
		if item.Group != nil {
			outcome.GroupID = item.Group.ID
		}
	}
	if err := workOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[WorkUpdate](addWorkUpdateStepType), WorkUpdate{ItemID: outcome.ItemID, Body: BuildUpdateBody(request, WorkItemScheduled)}), nil
}

// dex:group group-id:request group-label:"Work request"
// dex:explanation text:"Record the created item and prepare its update."
type recordCreatedItem struct {
	dex.StepDefaultsNoWaitFor[monday.CreateItemResult]
}

func (recordCreatedItem) GetStepType() string { return recordCreatedItemStepType }

func (recordCreatedItem) Execute(ctx dex.Context, result monday.CreateItemResult) (*dex.StepDecision, error) {
	request, err := workRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	// confirmExistingItem records an outcome only when it skipped a match; a missing value means none.
	previous, err := workOutcomeAttribute.Get(ctx)
	if err != nil && !isAttributeNotFound(err) {
		return nil, err
	}
	outcome := WorkRequestOutcome{
		Action: WorkItemCreated, ItemID: result.Value.ItemID, SkippedItemID: previous.SkippedItemID, WasCreateReplayed: result.Value.IsReplayed,
	}
	if item := result.Value.Item; item != nil {
		outcome.ItemURL = item.URL
		if item.Group != nil {
			outcome.GroupID = item.Group.ID
		}
	}
	if err := workOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[WorkUpdate](addWorkUpdateStepType), WorkUpdate{ItemID: outcome.ItemID, Body: BuildUpdateBody(request, WorkItemCreated)}), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"monday.com accepted the create but answered unusably; complete for a person to check the board instead of creating again."
type recordUncertainItem struct {
	dex.StepDefaultsNoWaitFor[monday.CreateItemResult]
}

func (recordUncertainItem) GetStepType() string { return recordUncertainItemStepType }

func (recordUncertainItem) Execute(ctx dex.Context, result monday.CreateItemResult) (*dex.StepDecision, error) {
	previous, err := workOutcomeAttribute.Get(ctx)
	if err != nil && !isAttributeNotFound(err) {
		return nil, err
	}
	outcome := WorkRequestOutcome{
		Action: WorkItemCreationUncertain, SkippedItemID: previous.SkippedItemID, NeedsReview: true,
		ReviewReason: createWorkItemReviewReason, ReviewDetail: failureMessage(result.Failure),
	}
	if err := workOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:request group-label:"Work request"
// dex:explanation text:"Record the update and complete the Flow."
type completeWorkRequest struct {
	dex.StepDefaultsNoWaitFor[monday.AddUpdateResult]
}

func (completeWorkRequest) GetStepType() string { return completeWorkRequestStepType }

func (completeWorkRequest) Execute(ctx dex.Context, result monday.AddUpdateResult) (*dex.StepDecision, error) {
	outcome, err := workOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.UpdateID = result.Value.UpdateID
	if err := workOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"monday.com accepted the update but answered unusably; complete for a person to check the item instead of posting again."
type recordUncertainUpdate struct {
	dex.StepDefaultsNoWaitFor[monday.AddUpdateResult]
}

func (recordUncertainUpdate) GetStepType() string { return recordUncertainUpdateStepType }

func (recordUncertainUpdate) Execute(ctx dex.Context, result monday.AddUpdateResult) (*dex.StepDecision, error) {
	outcome, err := workOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.NeedsReview, outcome.ReviewReason, outcome.ReviewDetail = true, addWorkUpdateReviewReason, failureMessage(result.Failure)
	if err := workOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
