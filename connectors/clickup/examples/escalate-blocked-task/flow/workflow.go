// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package escalateblocked escalates a ClickUp task that moves to the blocked status. The taskEvent
// Trigger starts one Flow per status change; the Flow reads the task, finds the escalation manager by
// email, looks for an escalation task already filed for it, creates one in the escalation List only
// when none exists, tags the blocked task, assigns the manager and raises its priority, and comments
// with a link to the escalation task.
package escalateblocked

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity shown in Dex Web.
	FlowType = "ClickUpEscalateBlockedTask"
	// ConnectionName is the static Dex Web connection for the ClickUp Workspace.
	ConnectionName = "clickup-workspace"
	// BlockedStatusTriggerBinding is the taskEvent binding whose events start this Flow.
	BlockedStatusTriggerBinding = "blocked-status"
	// FlowIDPrefix precedes the task ID and the status change's history item ID in every Flow ID.
	FlowIDPrefix = "clickup-escalation-"
	// EscalationTag marks escalation tasks, so the search finds them.
	EscalationTag = "escalation"
	// EscalatedTag marks a blocked task that was escalated.
	EscalatedTag = "escalated"
	// MaximumSearchedPages bounds the pages of escalation tasks searched for an existing one.
	MaximumSearchedPages = 5

	recordBlockedTaskStepType          = "RecordBlockedTask"
	readBlockedTaskStepType            = "ReadBlockedTask"
	chooseEscalationStepType           = "ChooseEscalation"
	findEscalationManagerStepType      = "FindEscalationManager"
	recordEscalationManagerStepType    = "RecordEscalationManager"
	completeWithoutManagerStepType     = "CompleteWithoutManager"
	searchExistingEscalationStepType   = "SearchExistingEscalation"
	chooseExistingEscalationStepType   = "ChooseExistingEscalation"
	createEscalationTaskStepType       = "CreateEscalationTask"
	recordEscalationTaskStepType       = "RecordEscalationTask"
	recordCreateNeedsReviewStepType    = "RecordCreateNeedsReview"
	tagBlockedTaskStepType             = "TagBlockedTask"
	chooseManagerAssignmentStepType    = "ChooseManagerAssignment"
	assignManagerToBlockedTaskStepType = "AssignManagerToBlockedTask"
	prepareEscalationCommentStepType   = "PrepareEscalationComment"
	commentOnBlockedTaskStepType       = "CommentOnBlockedTask"
	recordCommentNeedsReviewStepType   = "RecordCommentNeedsReview"
	completeEscalatedStepType          = "CompleteEscalated"

	escalationStateAttributeKey   = "clickup-escalation-state"
	escalationOutcomeAttributeKey = "clickup-escalation-outcome"
	maximumTaskNameInTitleBytes   = 900
)

var (
	escalationStateAttribute   = dex.DefineAttribute[EscalationState](escalationStateAttributeKey)
	escalationOutcomeAttribute = dex.DefineAttribute[EscalationOutcome](escalationOutcomeAttributeKey)
	numericIDPattern           = regexp.MustCompile(`^[0-9]{1,20}$`)
	taskIDPattern              = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
)

// EscalationSettings are the application's escalation rules, read once at Worker startup.
type EscalationSettings struct {
	// WorkspaceID is the ClickUp Workspace, the first number in a ClickUp web address.
	WorkspaceID string `json:"workspaceId"`
	// EscalationListID is the List escalation tasks are created in.
	EscalationListID string `json:"escalationListId"`
	// ManagerEmail is the email of the Workspace member who receives escalations.
	ManagerEmail string `json:"managerEmail"`
	// BlockedStatus is the status name that escalates a task, compared without case.
	BlockedStatus string `json:"blockedStatus"`
}

// Validate checks the settings before the Worker starts.
func (settings EscalationSettings) Validate() error {
	if !numericIDPattern.MatchString(settings.WorkspaceID) || !numericIDPattern.MatchString(settings.EscalationListID) {
		return errors.New("the Workspace and escalation List IDs must be ClickUp IDs of digits")
	}
	if address, err := mail.ParseAddress(settings.ManagerEmail); err != nil || address.Address != settings.ManagerEmail {
		return errors.New("the escalation manager email must be one bare email address")
	}
	if strings.TrimSpace(settings.BlockedStatus) == "" {
		return errors.New("the blocked status name is required")
	}
	return nil
}

// BlockedTask is the Flow's start input: one status change from the taskEvent Trigger, or a task ID
// entered in Dex Web Start Flow.
type BlockedTask struct {
	// TaskID is the ClickUp task ID.
	TaskID string `json:"taskId"`
	// EventID is the webhook event that started the Flow, or empty for Start Flow.
	EventID string `json:"eventId,omitempty"`
	// StatusAfter is the status the event reported, or empty for Start Flow.
	StatusAfter string `json:"statusAfter,omitempty"`
	// OccurredAt is when the status changed, or nil for Start Flow.
	OccurredAt *time.Time `json:"occurredAt,omitempty"`
}

// EscalationState is the escalation as the Flow learns about it.
type EscalationState struct {
	// Input is the validated start input.
	Input BlockedTask `json:"input"`
	// TaskName is the blocked task's name, once read.
	TaskName string `json:"taskName,omitempty"`
	// TaskURL is the blocked task's ClickUp web address, once read.
	TaskURL string `json:"taskUrl,omitempty"`
	// ManagerID is the escalation manager's user ID, once found.
	ManagerID int64 `json:"managerId,omitempty"`
	// ManagerName is the manager's display name, once found.
	ManagerName string `json:"managerName,omitempty"`
	// EscalationTaskID is the escalation task, once found or created.
	EscalationTaskID string `json:"escalationTaskId,omitempty"`
	// EscalationTaskURL is the escalation task's ClickUp web address, once found or created.
	EscalationTaskURL string `json:"escalationTaskUrl,omitempty"`
	// WasExistingEscalationFound reports that the search found an escalation task filed earlier.
	WasExistingEscalationFound bool `json:"wasExistingEscalationFound,omitempty"`
	// WasCreateAlreadyApplied reports that a retried create found its earlier attempt's task.
	WasCreateAlreadyApplied bool `json:"wasCreateAlreadyApplied,omitempty"`
	// BlockedTaskTags are the blocked task's tags after tagging.
	BlockedTaskTags []string `json:"blockedTaskTags,omitempty"`
}

// TaskReference names one task.
type TaskReference struct {
	// TaskID is the ClickUp task ID.
	TaskID string `json:"taskId"`
}

// ManagerLookup is the email the Flow looks up.
type ManagerLookup struct {
	// Email is the manager's email address.
	Email string `json:"email"`
}

// EscalationSearchPage is one page of the escalation List's tagged tasks to search.
type EscalationSearchPage struct {
	// Page is the zero-based page.
	Page int `json:"page"`
}

// EscalationRequest is the escalation task to create.
type EscalationRequest struct {
	// Name is the escalation task's name, which ends with the blocked task's ID in brackets.
	Name string `json:"name"`
	// ManagerID is the user the escalation task is assigned to.
	ManagerID int64 `json:"managerId"`
	// BlockedTaskURL is the blocked task's web address, linked from the description.
	BlockedTaskURL string `json:"blockedTaskUrl"`
}

// ManagerAssignment assigns the manager to the blocked task.
type ManagerAssignment struct {
	// TaskID is the blocked task.
	TaskID string `json:"taskId"`
	// ManagerID is the user to assign.
	ManagerID int64 `json:"managerId"`
}

// EscalationComment is the comment to add to the blocked task.
type EscalationComment struct {
	// TaskID is the blocked task.
	TaskID string `json:"taskId"`
	// Text is the comment's plain text.
	Text string `json:"text"`
}

// OutcomeAction is the Flow's terminal business outcome.
type OutcomeAction string

const (
	// OutcomeEscalated means the escalation task exists and the blocked task was tagged, assigned, and commented.
	OutcomeEscalated OutcomeAction = "escalated"
	// OutcomeSkipped means the task was not blocked or no manager was found, so nothing was written.
	OutcomeSkipped OutcomeAction = "skipped"
	// OutcomeNeedsReview means a create or comment could not be confirmed; a person checks ClickUp.
	OutcomeNeedsReview OutcomeAction = "needsReview"
)

// EscalationOutcome is the Flow result and the value of its outcome Attribute.
type EscalationOutcome struct {
	// Action is what the Flow did.
	Action OutcomeAction `json:"action"`
	// Reason explains skipped and needsReview, such as notBlocked, managerNotFound, or createUncertain.
	Reason string `json:"reason,omitempty"`
	// TaskID is the blocked task.
	TaskID string `json:"taskId"`
	// EscalationTaskID is the escalation task, when found or created.
	EscalationTaskID string `json:"escalationTaskId,omitempty"`
	// EscalationTaskURL is the escalation task's web address, when found or created.
	EscalationTaskURL string `json:"escalationTaskUrl,omitempty"`
	// ManagerID is the escalation manager's user ID, when found.
	ManagerID int64 `json:"managerId,omitempty"`
	// CommentID is the comment ClickUp added to the blocked task.
	CommentID string `json:"commentId,omitempty"`
	// WasExistingEscalationFound reports that no escalation task was created because one existed.
	WasExistingEscalationFound bool `json:"wasExistingEscalationFound,omitempty"`
	// WasCreateAlreadyApplied reports that a retried create found its earlier attempt's task.
	WasCreateAlreadyApplied bool `json:"wasCreateAlreadyApplied,omitempty"`
	// WasCommentAlreadyApplied reports that a retried comment found its earlier attempt's comment.
	WasCommentAlreadyApplied bool `json:"wasCommentAlreadyApplied,omitempty"`
	// Tags are the blocked task's tags after tagging.
	Tags []string `json:"tags,omitempty"`
	// ReviewDetail is the connector's safe failure message for needsReview.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// Flow escalates one blocked ClickUp task.
type Flow struct {
	dex.FlowDefaults
	connection clickup.Connection
	settings   EscalationSettings
}

// NewFlow binds the ClickUp Connection and the escalation settings. Settings that fail Validate fail
// each Flow before any ClickUp request.
func NewFlow(connection clickup.Connection, settings EscalationSettings) *Flow {
	return &Flow{connection: connection, settings: settings}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and ClickUp connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordBlockedTask{settings: flow.settings}),
		dex.DefineStep(clickup.NewGetTaskStep(clickup.GetTaskStepConfig[TaskReference]{
			StepType: readBlockedTaskStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "clickup", GroupLabel: "ClickUp", Explanation: "Read the task with its status, name, and web address."},
			Connection:  flow.connection, MapToOperationInput: MapToGetTaskInput,
			Found: sdkgo.GoTo(chooseEscalation{settings: flow.settings}),
		})),
		dex.DefineStep(chooseEscalation{settings: flow.settings}),
		dex.DefineStep(clickup.NewFindMemberByEmailStep(clickup.FindMemberByEmailStepConfig[ManagerLookup]{
			StepType: findEscalationManagerStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "clickup", GroupLabel: "ClickUp", Explanation: "Find the escalation manager among the Workspace's members by email."},
			Connection:  flow.connection, MapToOperationInput: flow.mapToFindMemberByEmailInput,
			Found:    sdkgo.GoTo(recordEscalationManager{}),
			NotFound: sdkgo.GoTo(completeWithoutManager{}),
		})),
		dex.DefineStep(recordEscalationManager{}),
		dex.DefineStep(completeWithoutManager{}),
		dex.DefineStep(clickup.NewSearchTasksStep(clickup.SearchTasksStepConfig[EscalationSearchPage]{
			StepType: searchExistingEscalationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "clickup", GroupLabel: "ClickUp", Explanation: "Search one page of the escalation List's tagged tasks, open or closed."},
			Connection:  flow.connection, MapToOperationInput: flow.mapToSearchTasksInput,
			Searched: sdkgo.GoTo(chooseExistingEscalation{}),
		})),
		dex.DefineStep(chooseExistingEscalation{}),
		dex.DefineStep(clickup.NewCreateTaskStep(clickup.CreateTaskStepConfig[EscalationRequest]{
			StepType: createEscalationTaskStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "clickup", GroupLabel: "ClickUp", Explanation: "Create the escalation task after a checkpoint, so a retry reads the List back instead of resending."},
			Connection:  flow.connection, MapToOperationInput: flow.mapToCreateTaskInput,
			Created:   sdkgo.GoTo(recordEscalationTask{}),
			Uncertain: sdkgo.GoTo(recordCreateNeedsReview{}),
		})),
		dex.DefineStep(recordEscalationTask{}),
		dex.DefineStep(recordCreateNeedsReview{}),
		dex.DefineStep(clickup.NewUpdateTaskTagsStep(clickup.UpdateTaskTagsStepConfig[TaskReference]{
			StepType: tagBlockedTaskStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "clickup", GroupLabel: "ClickUp", Explanation: "Tag the blocked task as escalated; a repeated tag changes nothing."},
			Connection:  flow.connection, MapToOperationInput: MapToUpdateTaskTagsInput,
			Updated: sdkgo.GoTo(chooseManagerAssignment{}),
		})),
		dex.DefineStep(chooseManagerAssignment{}),
		dex.DefineStep(clickup.NewUpdateTaskStep(clickup.UpdateTaskStepConfig[ManagerAssignment]{
			StepType: assignManagerToBlockedTaskStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "clickup", GroupLabel: "ClickUp", Explanation: "Assign the manager and set urgent priority; a repeated update leaves the same task."},
			Connection:  flow.connection, MapToOperationInput: MapToUpdateTaskInput,
			Updated: sdkgo.GoTo(prepareEscalationComment{}),
		})),
		dex.DefineStep(prepareEscalationComment{}),
		dex.DefineStep(clickup.NewAddCommentStep(clickup.AddCommentStepConfig[EscalationComment]{
			StepType: commentOnBlockedTaskStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "clickup", GroupLabel: "ClickUp", Explanation: "Comment on the blocked task with the escalation task's link after a checkpoint, so a retry reads the comments back instead of resending."},
			Connection:  flow.connection, MapToOperationInput: MapToAddCommentInput,
			Added:     sdkgo.GoTo(completeEscalated{}),
			Uncertain: sdkgo.GoTo(recordCommentNeedsReview{}),
		})),
		dex.DefineStep(recordCommentNeedsReview{}),
		dex.DefineStep(completeEscalated{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the escalation state and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{escalationStateAttribute, escalationOutcomeAttribute}}
}

// GetConnectorTriggerBindings declares the taskEvent binding that starts this Flow.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		clickup.DefineTaskEventTriggerBinding(clickup.TaskEventTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: BlockedStatusTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "events", UnitID: clickup.UIUnitTaskEventPicker, Label: "Events that start the Flow",
				Description: "Select taskStatusUpdated, the event ClickUp sends when a task's status changes, and subscribe the ClickUp webhook to it. The Flow itself admits only a change to the configured blocked status, so selecting none accepts every task event and still starts Flows only for blocked tasks.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: clickup.UITaskEventPickerPortEvents, JSONPointer: "/events"}},
			}}},
		}),
	}
}

// GetDexSummary returns the escalation state and outcome for the Dex Web run list.
//
// dex:field attribute-key:clickup-escalation-state value-type:json editable:false description:"Blocked task and escalation"
// dex:field attribute-key:clickup-escalation-outcome value-type:json editable:false description:"Escalation outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, outcome, err := escalationInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		escalationStateAttributeKey: state, escalationOutcomeAttributeKey: outcome,
	}}, nil
}

// GetDexDisplay returns the escalation state and outcome for the Dex Web run detail.
//
// dex:field attribute-key:clickup-escalation-state value-type:json editable:false description:"Task, manager, and escalation task"
// dex:field attribute-key:clickup-escalation-outcome value-type:json editable:false description:"Action, reason, escalation task, and comment"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	state, outcome, err := escalationInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		escalationStateAttributeKey: state, escalationOutcomeAttributeKey: outcome,
	}}, nil
}

// AcceptBlockedStatusEvent is the application's admission rule: only a change to the blocked status starts a Flow.
func (flow *Flow) AcceptBlockedStatusEvent(event sdkgo.TriggerEvent[clickup.TaskEvent]) bool {
	if event.Payload.Event != clickup.EventTaskStatusUpdated {
		return false
	}
	for _, item := range event.Payload.HistoryItems {
		if item.Field == "status" && isStatus(item.StatusAfter, flow.settings.BlockedStatus) {
			return true
		}
	}
	return false
}

// ResolveFlowID derives the Flow ID from the task and the status change, so a redelivery starts no
// second Flow while a later block of the same task starts a new one.
func ResolveFlowID(event sdkgo.TriggerEvent[clickup.TaskEvent]) string {
	changeID := event.Payload.TaskID
	if len(event.Payload.HistoryItems) != 0 {
		changeID = event.Payload.HistoryItems[0].ID
	}
	return FlowIDPrefix + event.Payload.TaskID + "-" + changeID
}

// MapToFlowInput copies the status change's identity into the Flow's start input.
func MapToFlowInput(event sdkgo.TriggerEvent[clickup.TaskEvent]) BlockedTask {
	occurredAt := event.OccurredAt
	input := BlockedTask{TaskID: event.Payload.TaskID, EventID: event.ID, OccurredAt: &occurredAt}
	for _, item := range event.Payload.HistoryItems {
		if item.Field == "status" {
			input.StatusAfter = item.StatusAfter
			break
		}
	}
	return input
}

// MapToGetTaskInput reads the blocked task.
func MapToGetTaskInput(reference TaskReference) clickup.GetTaskInput {
	return clickup.GetTaskInput{TaskID: reference.TaskID}
}

// mapToFindMemberByEmailInput looks the manager up in the configured Workspace.
func (flow *Flow) mapToFindMemberByEmailInput(lookup ManagerLookup) clickup.FindMemberByEmailInput {
	return clickup.FindMemberByEmailInput{WorkspaceID: flow.settings.WorkspaceID, Email: lookup.Email}
}

// mapToSearchTasksInput searches the escalation List's tagged tasks, including closed ones.
func (flow *Flow) mapToSearchTasksInput(page EscalationSearchPage) clickup.SearchTasksInput {
	return clickup.SearchTasksInput{
		WorkspaceID: flow.settings.WorkspaceID, ListIDs: []string{flow.settings.EscalationListID}, Tags: []string{EscalationTag},
		IsClosedIncluded: true, AreSubtasksIncluded: true, Page: page.Page,
	}
}

// mapToCreateTaskInput files the escalation task in the escalation List, assigned to the manager.
func (flow *Flow) mapToCreateTaskInput(request EscalationRequest) clickup.CreateTaskInput {
	return clickup.CreateTaskInput{
		ListID: flow.settings.EscalationListID, Name: request.Name, Priority: clickup.TaskPriorityUrgent,
		MarkdownDescription: "[The blocked task](" + request.BlockedTaskURL + ") moved to the **" + flow.settings.BlockedStatus + "** status and needs a decision.",
		AssigneeIDs:         []int64{request.ManagerID}, Tags: []string{EscalationTag},
	}
}

// MapToUpdateTaskTagsInput tags the blocked task.
func MapToUpdateTaskTagsInput(reference TaskReference) clickup.UpdateTaskTagsInput {
	return clickup.UpdateTaskTagsInput{TaskID: reference.TaskID, AddTags: []string{EscalatedTag}}
}

// MapToUpdateTaskInput assigns the manager and raises the blocked task's priority to urgent.
func MapToUpdateTaskInput(assignment ManagerAssignment) clickup.UpdateTaskInput {
	return clickup.UpdateTaskInput{TaskID: assignment.TaskID, Priority: clickup.TaskPriorityUrgent, AddAssigneeIDs: []int64{assignment.ManagerID}}
}

// MapToAddCommentInput comments on the blocked task.
func MapToAddCommentInput(comment EscalationComment) clickup.AddCommentInput {
	return clickup.AddCommentInput{TaskID: comment.TaskID, Text: comment.Text}
}

// EscalationTaskName names the escalation task after the blocked task and ends it with the blocked
// task's ID in brackets, which FindExistingEscalation and createTask's read-back both rely on.
func EscalationTaskName(taskID string, taskName string) string {
	title := strings.TrimSpace(taskName)
	if len(title) > maximumTaskNameInTitleBytes {
		title = strings.ToValidUTF8(title[:maximumTaskNameInTitleBytes], "")
	}
	return "Escalation: " + title + " [" + taskID + "]"
}

// FindExistingEscalation returns the escalation task filed for taskID, identified by its name's suffix.
func FindExistingEscalation(tasks []clickup.Task, taskID string) (clickup.Task, bool) {
	for _, task := range tasks {
		if strings.HasSuffix(strings.TrimSpace(task.Name), "["+taskID+"]") {
			return task, true
		}
	}
	return clickup.Task{}, false
}

// EscalationCommentText is the comment on the blocked task.
func EscalationCommentText(managerName string, escalationTaskURL string) string {
	return "Escalated to " + managerName + ": " + escalationTaskURL
}

func isStatus(status string, blockedStatus string) bool {
	return strings.EqualFold(strings.TrimSpace(status), strings.TrimSpace(blockedStatus))
}

func escalationInspection(ctx dex.Context) (EscalationState, EscalationOutcome, error) {
	state, err := optionalAttribute(ctx, escalationStateAttribute)
	if err != nil {
		return EscalationState{}, EscalationOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, escalationOutcomeAttribute)
	if err != nil {
		return EscalationState{}, EscalationOutcome{}, err
	}
	return state, outcome, nil
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

// dex:group group-id:blocked group-label:"Blocked task"
// dex:explanation text:"Validate the task ID and the escalation settings, and record the blocked task."
type recordBlockedTask struct {
	dex.StepDefaults
	settings EscalationSettings
}

func (recordBlockedTask) GetStepType() string { return recordBlockedTaskStepType }

// WaitFor skips immediately because Dex Web Start Flow invokes the start Step's WaitFor.
func (recordBlockedTask) WaitFor(dex.Context, BlockedTask) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordBlockedTask) Execute(ctx dex.Context, input BlockedTask) (*dex.StepDecision, error) {
	input.TaskID = strings.TrimSpace(input.TaskID)
	if !taskIDPattern.MatchString(input.TaskID) {
		return dex.ForceFail("taskId must be a ClickUp task ID, the code after /t/ in the task's web address"), nil
	}
	if err := step.settings.Validate(); err != nil {
		return dex.ForceFail("set the CLICKUP_ escalation settings described in the example README and restart the Worker: " + err.Error()), nil
	}
	if err := escalationStateAttribute.Set(ctx, EscalationState{Input: input}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TaskReference](readBlockedTaskStepType), TaskReference{TaskID: input.TaskID}), nil
}

// dex:group group-id:blocked group-label:"Blocked task"
// dex:explanation text:"Skip a task that is no longer in the blocked status; otherwise look up the escalation manager."
type chooseEscalation struct {
	dex.StepDefaultsNoWaitFor[clickup.GetTaskResult]
	settings EscalationSettings
}

func (chooseEscalation) GetStepType() string { return chooseEscalationStepType }

func (step chooseEscalation) Execute(ctx dex.Context, result clickup.GetTaskResult) (*dex.StepDecision, error) {
	state, err := escalationStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if !isStatus(result.Value.Status.Name, step.settings.BlockedStatus) {
		outcome := EscalationOutcome{Action: OutcomeSkipped, Reason: "notBlocked", TaskID: state.Input.TaskID}
		if err := escalationOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	state.TaskName, state.TaskURL = result.Value.Name, result.Value.URL
	if err := escalationStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ManagerLookup](findEscalationManagerStepType), ManagerLookup{Email: step.settings.ManagerEmail}), nil
}

// dex:group group-id:blocked group-label:"Blocked task"
// dex:explanation text:"Record the escalation manager and search the escalation List from its first page."
type recordEscalationManager struct {
	dex.StepDefaultsNoWaitFor[clickup.FindMemberByEmailResult]
}

func (recordEscalationManager) GetStepType() string { return recordEscalationManagerStepType }

func (recordEscalationManager) Execute(ctx dex.Context, result clickup.FindMemberByEmailResult) (*dex.StepDecision, error) {
	state, err := escalationStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.ManagerID, state.ManagerName = result.Value.ID, result.Value.Username
	if err := escalationStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[EscalationSearchPage](searchExistingEscalationStepType), EscalationSearchPage{Page: 0}), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Complete without writing when no Workspace member has the manager's email."
type completeWithoutManager struct {
	dex.StepDefaultsNoWaitFor[clickup.FindMemberByEmailResult]
}

func (completeWithoutManager) GetStepType() string { return completeWithoutManagerStepType }

func (completeWithoutManager) Execute(ctx dex.Context, _ clickup.FindMemberByEmailResult) (*dex.StepDecision, error) {
	state, err := escalationStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := EscalationOutcome{Action: OutcomeSkipped, Reason: "managerNotFound", TaskID: state.Input.TaskID}
	if err := escalationOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:escalation group-label:"Escalation task"
// dex:explanation text:"Reuse an escalation task already filed for this task, search the next page, or create one."
type chooseExistingEscalation struct {
	dex.StepDefaultsNoWaitFor[clickup.SearchTasksResult]
}

func (chooseExistingEscalation) GetStepType() string { return chooseExistingEscalationStepType }

func (chooseExistingEscalation) Execute(ctx dex.Context, result clickup.SearchTasksResult) (*dex.StepDecision, error) {
	state, err := escalationStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if existing, isFound := FindExistingEscalation(result.Value.Tasks, state.Input.TaskID); isFound {
		state.EscalationTaskID, state.EscalationTaskURL, state.WasExistingEscalationFound = existing.ID, existing.URL, true
		if err := escalationStateAttribute.Set(ctx, state); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[TaskReference](tagBlockedTaskStepType), TaskReference{TaskID: state.Input.TaskID}), nil
	}
	if result.Value.NextPage != nil && *result.Value.NextPage < MaximumSearchedPages {
		return dex.GoTo(sdkgo.StepRef[EscalationSearchPage](searchExistingEscalationStepType), EscalationSearchPage{Page: *result.Value.NextPage}), nil
	}
	return dex.GoTo(sdkgo.StepRef[EscalationRequest](createEscalationTaskStepType), EscalationRequest{
		Name: EscalationTaskName(state.Input.TaskID, state.TaskName), ManagerID: state.ManagerID, BlockedTaskURL: state.TaskURL,
	}), nil
}

// dex:group group-id:escalation group-label:"Escalation task"
// dex:explanation text:"Record the escalation task ClickUp created, or that a retried create found, then tag the blocked task."
type recordEscalationTask struct {
	dex.StepDefaultsNoWaitFor[clickup.CreateTaskResult]
}

func (recordEscalationTask) GetStepType() string { return recordEscalationTaskStepType }

func (recordEscalationTask) Execute(ctx dex.Context, result clickup.CreateTaskResult) (*dex.StepDecision, error) {
	state, err := escalationStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.EscalationTaskID, state.EscalationTaskURL = result.Value.Task.ID, result.Value.Task.URL
	state.WasCreateAlreadyApplied = result.Value.WasAlreadyApplied
	if err := escalationStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TaskReference](tagBlockedTaskStepType), TaskReference{TaskID: state.Input.TaskID}), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Record an unconfirmed create for a person to check and complete without touching the blocked task."
type recordCreateNeedsReview struct {
	dex.StepDefaultsNoWaitFor[clickup.CreateTaskResult]
}

func (recordCreateNeedsReview) GetStepType() string { return recordCreateNeedsReviewStepType }

func (recordCreateNeedsReview) Execute(ctx dex.Context, result clickup.CreateTaskResult) (*dex.StepDecision, error) {
	state, err := escalationStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := EscalationOutcome{Action: OutcomeNeedsReview, Reason: "createUncertain", TaskID: state.Input.TaskID, ManagerID: state.ManagerID}
	if result.Failure != nil {
		outcome.ReviewDetail = result.Failure.Message
	}
	if err := escalationOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:blocked group-label:"Blocked task"
// dex:explanation text:"Record the blocked task's tags and assign the manager to it."
type chooseManagerAssignment struct {
	dex.StepDefaultsNoWaitFor[clickup.UpdateTaskTagsResult]
}

func (chooseManagerAssignment) GetStepType() string { return chooseManagerAssignmentStepType }

func (chooseManagerAssignment) Execute(ctx dex.Context, result clickup.UpdateTaskTagsResult) (*dex.StepDecision, error) {
	state, err := escalationStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state.BlockedTaskTags = result.Value.Tags
	if err := escalationStateAttribute.Set(ctx, state); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ManagerAssignment](assignManagerToBlockedTaskStepType), ManagerAssignment{
		TaskID: state.Input.TaskID, ManagerID: state.ManagerID,
	}), nil
}

// dex:group group-id:blocked group-label:"Blocked task"
// dex:explanation text:"Write the comment that links the blocked task to its escalation task."
type prepareEscalationComment struct {
	dex.StepDefaultsNoWaitFor[clickup.UpdateTaskResult]
}

func (prepareEscalationComment) GetStepType() string { return prepareEscalationCommentStepType }

func (prepareEscalationComment) Execute(ctx dex.Context, _ clickup.UpdateTaskResult) (*dex.StepDecision, error) {
	state, err := escalationStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[EscalationComment](commentOnBlockedTaskStepType), EscalationComment{
		TaskID: state.Input.TaskID, Text: EscalationCommentText(state.ManagerName, state.EscalationTaskURL),
	}), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Record an unconfirmed comment for a person to check and complete."
type recordCommentNeedsReview struct {
	dex.StepDefaultsNoWaitFor[clickup.AddCommentResult]
}

func (recordCommentNeedsReview) GetStepType() string { return recordCommentNeedsReviewStepType }

func (recordCommentNeedsReview) Execute(ctx dex.Context, result clickup.AddCommentResult) (*dex.StepDecision, error) {
	state, err := escalationStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := EscalationOutcome{
		Action: OutcomeNeedsReview, Reason: "commentUncertain", TaskID: state.Input.TaskID, EscalationTaskID: state.EscalationTaskID,
		EscalationTaskURL: state.EscalationTaskURL, ManagerID: state.ManagerID, Tags: state.BlockedTaskTags,
	}
	if result.Failure != nil {
		outcome.ReviewDetail = result.Failure.Message
	}
	if err := escalationOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Record the escalation as the outcome and complete the Flow."
type completeEscalated struct {
	dex.StepDefaultsNoWaitFor[clickup.AddCommentResult]
}

func (completeEscalated) GetStepType() string { return completeEscalatedStepType }

func (completeEscalated) Execute(ctx dex.Context, result clickup.AddCommentResult) (*dex.StepDecision, error) {
	state, err := escalationStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := EscalationOutcome{
		Action: OutcomeEscalated, TaskID: state.Input.TaskID, EscalationTaskID: state.EscalationTaskID, EscalationTaskURL: state.EscalationTaskURL,
		ManagerID: state.ManagerID, CommentID: result.Value.CommentID, WasExistingEscalationFound: state.WasExistingEscalationFound,
		WasCreateAlreadyApplied: state.WasCreateAlreadyApplied, WasCommentAlreadyApplied: result.Value.WasAlreadyApplied, Tags: state.BlockedTaskTags,
	}
	if err := escalationOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
