// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package approvedrequesttask demonstrates every Asana operation in one Flow started from Dex Web Start
// Flow: turn an approved request into an Asana task, assign it and move it to the picked section, and comment
// with the approval. The Flow reuses an open task that already carries the request ID, and parks an uncertain
// create for an operator instead of creating the task again.
package approvedrequesttask

import (
	"errors"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "AsanaApprovedRequestTask"
	// ConnectionName is the static Dex Web connection for Asana.
	ConnectionName = "asana-requests"
	// ReconcileRequestTaskPermission is required by both reconciliation Actions.
	ReconcileRequestTaskPermission = "asana-request-task.reconcile"

	recordApprovedRequestStepType     = "RecordApprovedRequest"
	findOpenRequestTasksStepType      = "FindOpenRequestTasks"
	evaluateOpenRequestTasksStepType  = "EvaluateOpenRequestTasks"
	createRequestTaskStepType         = "CreateRequestTask"
	recordCreatedTaskStepType         = "RecordCreatedTask"
	recordRejectedTaskStepType        = "RecordRejectedTask"
	recordUncertainTaskStepType       = "RecordUncertainTask"
	readReportedTaskStepType          = "ReadReportedTask"
	verifyReportedTaskStepType        = "VerifyReportedTask"
	recordMissingReportedTaskStepType = "RecordMissingReportedTask"
	assignRequestTaskStepType         = "AssignRequestTask"
	recordAssignedTaskStepType        = "RecordAssignedTask"
	recordRejectedAssignmentStepType  = "RecordRejectedAssignment"
	commentOnRequestTaskStepType      = "CommentOnRequestTask"
	recordApprovalCommentStepType     = "RecordApprovalComment"

	// MaximumOpenTaskPages bounds the duplicate check to 500 open tasks in the project.
	MaximumOpenTaskPages = 5
	openTaskPageSize     = 100
	maximumRequestIDSize = 64
)

// Request task phases stored in the asana-request-task-phase Attribute.
const (
	// PhaseSearching means the Flow is looking for an open task that already carries the request ID.
	PhaseSearching = "searching"
	// PhaseCreating means a createTask Step is about to run or running.
	PhaseCreating = "creating"
	// PhaseAssigning means the Flow is assigning the task and moving it to the request section.
	PhaseAssigning = "assigning"
	// PhaseCommenting means the Flow is adding the approval comment.
	PhaseCommenting = "commenting"
	// PhaseReady means the task exists, is assigned and in its section, and carries the approval comment.
	PhaseReady = "ready"
	// PhaseRejected means Asana conclusively rejected the create and nothing was created.
	PhaseRejected = "rejected"
	// PhaseAssignmentRejected means the task exists but Asana rejected the assignee, due date, or section.
	PhaseAssignmentRejected = "assignmentRejected"
	// PhaseNeedsReconciliation means the create outcome is unknown and an operator must confirm or retry.
	PhaseNeedsReconciliation = "needsReconciliation"
	// PhaseVerifyingReportedTask means the Flow is reading the task an operator reported.
	PhaseVerifyingReportedTask = "verifyingReportedTask"
	// PhaseDuplicateCheckIncomplete means the project has more open tasks than the duplicate check reads.
	PhaseDuplicateCheckIncomplete = "duplicateCheckIncomplete"
)

// Reconciliation notes explain why a reported task ID was not adopted.
const (
	// NoteReportedTaskMissing means Asana has no visible task with the reported ID.
	NoteReportedTaskMissing = "Asana has no visible task with the reported ID"
	// NoteReportedTaskMismatch means the reported task is in another project or lacks the request ID.
	NoteReportedTaskMismatch = "the reported task is in another project or its name lacks the request ID"
)

var (
	requestTaskPhaseAttribute = dex.DefineAttribute[string]("asana-request-task-phase")
	requestTaskAttribute      = dex.DefineAttribute[RequestTask]("asana-request-task")
)

var errReportedTaskIDInvalid = errors.New("taskId must be an Asana task gid, the number in the task's URL")

// Input is the approved request entered in Dex Web Start Flow.
type Input struct {
	// RequestID is the approved request's stable ID, such as REQ-1042; the task name starts with it in brackets.
	RequestID string `json:"requestId"`
	// Title is the request title, such as Replace badge reader at door 4.
	Title string `json:"title"`
	// Details is the plain-text task description.
	Details string `json:"details,omitempty"`
	// ApprovedBy names the approver for the approval comment.
	ApprovedBy string `json:"approvedBy"`
	// ApprovalNote is added to the approval comment; blank adds nothing.
	ApprovalNote string `json:"approvalNote,omitempty"`
	// AssigneeID is me, an Asana user gid, or a workspace member's email address.
	AssigneeID string `json:"assigneeId"`
	// DueOn is the due date as YYYY-MM-DD; blank leaves the due date unchanged.
	DueOn string `json:"dueOn,omitempty"`
	// ProjectID names the project gid when no project was picked in Dex Web.
	ProjectID string `json:"projectId,omitempty"`
	// SectionID names the section gid when no section was picked in Dex Web; blank keeps Asana's default.
	SectionID string `json:"sectionId,omitempty"`
}

// ProjectSelection is the value the project picker saves for the CreateRequestTask Step.
type ProjectSelection struct {
	// WorkspaceID is the picked project's workspace gid.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// ProjectID is the picked project's gid; blank uses the Start Flow projectId.
	ProjectID string `json:"projectId,omitempty"`
	// ProjectName is the picked project's name.
	ProjectName string `json:"projectName,omitempty"`
	// SectionID is the picked section's gid; blank uses the Start Flow sectionId.
	SectionID string `json:"sectionId,omitempty"`
	// SectionName is the picked section's name.
	SectionName string `json:"sectionName,omitempty"`
}

// TaskRequest is the validated request every later Step reads.
type TaskRequest struct {
	// RequestID is the approved request's stable ID.
	RequestID string `json:"requestId"`
	// TaskName is the task name: the request ID in brackets, then the title.
	TaskName string `json:"taskName"`
	// Notes is the plain-text task description.
	Notes string `json:"notes,omitempty"`
	// ProjectID is the project the task belongs to.
	ProjectID string `json:"projectId"`
	// SectionID is the section an approved task belongs in; blank keeps Asana's default section.
	SectionID string `json:"sectionId,omitempty"`
	// AssigneeID is me, a user gid, or an email address.
	AssigneeID string `json:"assigneeId"`
	// DueOn is the due date as YYYY-MM-DD, or blank.
	DueOn string `json:"dueOn,omitempty"`
	// Comment is the approval comment text.
	Comment string `json:"comment"`
}

// OpenTaskPage selects one page of the project's open tasks for the duplicate check.
type OpenTaskPage struct {
	// ProjectID is the project to list.
	ProjectID string `json:"projectId"`
	// Offset continues the previous page; blank reads the first.
	Offset string `json:"offset,omitempty"`
}

// TaskReference identifies the task an operator reported.
type TaskReference struct {
	// TaskID is the reported task's gid.
	TaskID string `json:"taskId"`
}

// TaskAssignment is one updateTask call: assign, set the due date, and move to the section.
type TaskAssignment struct {
	// TaskID is the task to update.
	TaskID string `json:"taskId"`
	// AssigneeID is the assignee.
	AssigneeID string `json:"assigneeId"`
	// DueOn is the due date, or blank to leave it.
	DueOn string `json:"dueOn,omitempty"`
	// SectionID is the section to move the task to, or blank to leave its sections.
	SectionID string `json:"sectionId,omitempty"`
}

// CommentRequest is one addComment call.
type CommentRequest struct {
	// TaskID is the task to comment on.
	TaskID string `json:"taskId"`
	// Text is the plain-text comment.
	Text string `json:"text"`
}

// UncertainCreate records a dispatched create whose outcome Asana did not confirm.
type UncertainCreate struct {
	// CallID is the connector call identity of the uncertain create.
	CallID string `json:"callId"`
	// ObservedAt is when the connector observed the unknown outcome; look for the task around it.
	ObservedAt time.Time `json:"observedAt"`
	// FailureKind is the safe connector failure category, such as TRANSPORT.
	FailureKind sdkgo.FailureKind `json:"failureKind"`
}

// RequestTask is the Flow's durable record of one approved request's task.
type RequestTask struct {
	// Request is the validated request.
	Request TaskRequest `json:"request"`
	// Phase mirrors the asana-request-task-phase Attribute.
	Phase string `json:"phase"`
	// OpenTaskPagesRead counts listTasks pages read by the duplicate check.
	OpenTaskPagesRead int `json:"openTaskPagesRead"`
	// CreateAttempts counts createTask Step executions: the first create plus each approved retry.
	CreateAttempts int `json:"createAttempts"`
	// IsExistingTaskReused reports that the duplicate check found an open task with the request ID.
	IsExistingTaskReused bool `json:"isExistingTaskReused,omitempty"`
	// TaskID is the task's gid once it is known.
	TaskID string `json:"taskId,omitempty"`
	// Task is the task read back after the assignment.
	Task *asana.Task `json:"task,omitempty"`
	// CommentID is the approval comment's story gid once Asana confirmed it.
	CommentID string `json:"commentId,omitempty"`
	// IsCommentOutcomeUnknown reports a comment Asana may or may not have stored; it is never re-sent.
	IsCommentOutcomeUnknown bool `json:"isCommentOutcomeUnknown,omitempty"`
	// UncertainCreate describes the latest uncertain create while reconciliation is pending.
	UncertainCreate *UncertainCreate `json:"uncertainCreate,omitempty"`
	// ReconciliationNote explains why a reported task ID was not adopted.
	ReconciliationNote string `json:"reconciliationNote,omitempty"`
}

// ConfirmCreatedTaskInput reports the task an operator found after an uncertain create.
type ConfirmCreatedTaskInput struct {
	// TaskID is the gid of the task found in the project, the number in its Asana URL.
	TaskID string `json:"taskId"`
}

// Flow turns one approved request into an assigned, commented Asana task.
type Flow struct {
	dex.FlowDefaults
	connection asana.Connection
	selection  ProjectSelection
}

// NewFlow binds the Asana Connection and the picked project at registration time. A blank selection uses each
// Start Flow input's projectId and sectionId.
func NewFlow(connection asana.Connection, selection ProjectSelection) *Flow {
	selection.ProjectID = strings.TrimSpace(selection.ProjectID)
	selection.SectionID = strings.TrimSpace(selection.SectionID)
	return &Flow{connection: connection, selection: selection}
}

// ProjectSelectionConfigurationRef identifies the project picker value saved in Dex Web.
func ProjectSelectionConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: asana.ConnectorID, ConnectionName: ConnectionName, OperationID: "createTask",
		FlowType: FlowType, StepType: createRequestTaskStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Asana connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordApprovedRequest{selection: flow.selection}),
		dex.DefineStep(asana.NewListTasksStep(asana.ListTasksStepConfig[OpenTaskPage]{
			StepType: findOpenRequestTasksStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "asana", GroupLabel: "Asana",
				Explanation: "List one page of the project's incomplete tasks to find one that already carries the request ID.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListTasksInput,
			Listed: sdkgo.GoTo(evaluateOpenRequestTasks{}),
		})),
		dex.DefineStep(evaluateOpenRequestTasks{}),
		dex.DefineStep(asana.NewCreateTaskStep(asana.CreateTaskStepConfig[TaskRequest]{
			StepType: createRequestTaskStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "asana", GroupLabel: "Asana",
				Explanation: "Create the task once; an unknown outcome is reconciled, never created again automatically.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "requestProject", UnitID: asana.UIUnitProjectPicker, Label: "Request project and section",
				Description: "Choose the Asana project that receives approved request tasks and, optionally, the section an approved task belongs in; the duplicate check lists this project's incomplete tasks, and assignment moves a reused task to the same section. Leave it unsaved to use each Start Flow input's projectId and sectionId; a blank section keeps Asana's default section. Restart the Worker after saving.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: asana.UIProjectPickerPortWorkspaceID, JSONPointer: "/workspaceId"},
					{Port: asana.UIProjectPickerPortProjectID, JSONPointer: "/projectId"},
					{Port: asana.UIProjectPickerPortProjectName, JSONPointer: "/projectName"},
					{Port: asana.UIProjectPickerPortSectionID, JSONPointer: "/sectionId"},
					{Port: asana.UIProjectPickerPortSectionName, JSONPointer: "/sectionName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: MapToCreateTaskInput,
			Created:          sdkgo.GoTo(recordCreatedTask{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedTask{}),
			Uncertain:        sdkgo.GoTo(recordUncertainTask{}),
		})),
		dex.DefineStep(recordCreatedTask{}),
		dex.DefineStep(recordRejectedTask{}),
		dex.DefineStep(recordUncertainTask{}),
		dex.DefineStep(asana.NewGetTaskStep(asana.GetTaskStepConfig[TaskReference]{
			StepType: readReportedTaskStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "asana", GroupLabel: "Asana",
				Explanation: "Read the task an operator reported to confirm its project and request ID.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetTaskInput,
			Found:    sdkgo.GoTo(verifyReportedTask{}),
			NotFound: sdkgo.GoTo(recordMissingReportedTask{}),
		})),
		dex.DefineStep(verifyReportedTask{}),
		dex.DefineStep(recordMissingReportedTask{}),
		dex.DefineStep(asana.NewUpdateTaskStep(asana.UpdateTaskStepConfig[TaskAssignment]{
			StepType: assignRequestTaskStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "asana", GroupLabel: "Asana",
				Explanation: "Assign the task, set its due date, and move it to the request section; a repeated update leaves the same task.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateTaskInput,
			Updated:          sdkgo.GoTo(recordAssignedTask{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedAssignment{}),
		})),
		dex.DefineStep(recordAssignedTask{}),
		dex.DefineStep(recordRejectedAssignment{}),
		dex.DefineStep(asana.NewAddCommentStep(asana.AddCommentStepConfig[CommentRequest]{
			StepType: commentOnRequestTaskStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "asana", GroupLabel: "Asana",
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
	reconciliationLocks := []dex.AttributeLock{dex.LockAttribute(requestTaskPhaseAttribute), dex.LockAttribute(requestTaskAttribute)}
	return []dex.RPCDef{
		dex.DefineRPC(flow.ConfirmCreatedTask, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Confirm created task",
				dex.WhenAttributeMatches(requestTaskPhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsReconciliation)),
				dex.ActionRequiresPermission(ReconcileRequestTaskPermission),
			),
			LockAttributes: reconciliationLocks,
		}),
		dex.DefineRPC(flow.ApproveTaskCreateRetry, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Create task again",
				dex.WhenAttributeMatches(requestTaskPhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsReconciliation)),
				dex.ActionRequiresPermission(ReconcileRequestTaskPermission),
			),
			LockAttributes: reconciliationLocks,
		}),
		dex.DefineRPC(flow.GetRequestTask, nil),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the phase and record Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{requestTaskPhaseAttribute, requestTaskAttribute}}
}

// MapToListTasksInput lists one page of the project's incomplete tasks.
func MapToListTasksInput(page OpenTaskPage) asana.ListTasksInput {
	return asana.ListTasksInput{ProjectID: page.ProjectID, IsIncompleteOnly: true, PageSize: openTaskPageSize, Offset: page.Offset}
}

// MapToCreateTaskInput maps the recorded request to one new task; AssignRequestTask assigns it next.
func MapToCreateTaskInput(request TaskRequest) asana.CreateTaskInput {
	return asana.CreateTaskInput{
		Name: request.TaskName, Notes: request.Notes, ProjectID: request.ProjectID, SectionID: request.SectionID, DueOn: request.DueOn,
	}
}

// MapToGetTaskInput reads the task an operator reported.
func MapToGetTaskInput(reference TaskReference) asana.GetTaskInput {
	return asana.GetTaskInput{TaskID: reference.TaskID}
}

// MapToUpdateTaskInput assigns the task, sets a requested due date, and moves it to the request section.
func MapToUpdateTaskInput(assignment TaskAssignment) asana.UpdateTaskInput {
	input := asana.UpdateTaskInput{TaskID: assignment.TaskID, SectionID: assignment.SectionID}
	assigneeID := assignment.AssigneeID
	input.AssigneeID = &assigneeID
	if assignment.DueOn != "" {
		dueOn := assignment.DueOn
		input.DueOn = &dueOn
	}
	return input
}

// MapToAddCommentInput maps the approval comment to one Asana comment.
func MapToAddCommentInput(request CommentRequest) asana.AddCommentInput {
	return asana.AddCommentInput{TaskID: request.TaskID, Text: request.Text}
}

// TaskNamePrefix is the bracketed request ID every request task name starts with, such as [REQ-1042].
func TaskNamePrefix(requestID string) string { return "[" + requestID + "]" }

// FindRequestTask returns the first incomplete task whose name starts with the request's bracketed ID.
// [REQ-10420] and a name that only mentions REQ-1042 elsewhere are not the same request.
func FindRequestTask(tasks []asana.Task, requestID string) (asana.Task, bool) {
	prefix := TaskNamePrefix(requestID)
	for _, task := range tasks {
		if !task.IsCompleted && strings.HasPrefix(strings.TrimSpace(task.Name), prefix) {
			return task, true
		}
	}
	return asana.Task{}, false
}

// ConfirmCreatedTask adopts a task an operator found in the project after an uncertain create. The Flow reads
// it with getTask and adopts it only when it is in the project and its name starts with the request ID.
//
// dex:input field-name:taskId value-type:string source:user required:true description:"Gid of the task found in the project, the number in its Asana URL"
func (*Flow) ConfirmCreatedTask(ctx dex.Context, input ConfirmCreatedTaskInput) (*dex.RPCResult[dex.None], error) {
	record, err := requestTaskAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if record.Phase != PhaseNeedsReconciliation {
		return &dex.RPCResult[dex.None]{}, nil
	}
	taskID := strings.TrimSpace(input.TaskID)
	if !isTaskGID(taskID) {
		return nil, errReportedTaskIDInvalid
	}
	record.Phase = PhaseVerifyingReportedTask
	record.ReconciliationNote = ""
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[TaskReference](readReportedTaskStepType), TaskReference{TaskID: taskID})},
	}, nil
}

// ApproveTaskCreateRetry creates the task again after an operator confirmed the project has no such task. The
// retry is a new Step execution with a new connector call ID.
func (*Flow) ApproveTaskCreateRetry(ctx dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	record, err := requestTaskAttribute.Get(ctx)
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
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[TaskRequest](createRequestTaskStepType), record.Request)},
	}, nil
}

// GetRequestTask returns the current record.
func (*Flow) GetRequestTask(ctx dex.Context, _ dex.None) (*dex.RPCResult[RequestTask], error) {
	record, err := requestTaskAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[RequestTask]{Output: record}, nil
}

// GetDexSummary returns the phase and record for Dex Web lists.
//
// dex:field attribute-key:asana-request-task-phase value-type:string editable:false description:"Request task phase"
// dex:field attribute-key:asana-request-task value-type:json editable:false description:"Request, task ID, assignment, and approval comment"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, requestTaskPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, requestTaskAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"asana-request-task-phase": phase,
		"asana-request-task":       record,
	}}, nil
}

// GetDexDisplay returns the phase and record for the Dex Web run view.
//
// dex:field attribute-key:asana-request-task-phase value-type:string editable:false description:"Request task phase" ui-slot:status
// dex:field attribute-key:asana-request-task value-type:json editable:false description:"Request, assigned task read back, approval comment, and any uncertain create awaiting reconciliation"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, requestTaskPhaseAttribute)
	if err != nil {
		return nil, err
	}
	record, err := optionalAttribute(ctx, requestTaskAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"asana-request-task-phase": phase,
		"asana-request-task":       record,
	}}, nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Validate the approved request and record it with the picked project before calling Asana."
type recordApprovedRequest struct {
	dex.StepDefaults
	selection ProjectSelection
}

func (recordApprovedRequest) GetStepType() string { return recordApprovedRequestStepType }

func (recordApprovedRequest) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestTaskPhaseAttribute), dex.LockAttribute(requestTaskAttribute)}}
}

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordApprovedRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordApprovedRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildTaskRequest(step.selection, input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	record := RequestTask{Request: request, Phase: PhaseSearching}
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[OpenTaskPage](findOpenRequestTasksStepType), OpenTaskPage{ProjectID: request.ProjectID}), nil
}

// BuildTaskRequest validates Start Flow input. A picked project and section win over the input's IDs.
func BuildTaskRequest(selection ProjectSelection, input Input) (TaskRequest, error) {
	request := TaskRequest{
		RequestID: strings.TrimSpace(input.RequestID), Notes: input.Details,
		ProjectID: strings.TrimSpace(selection.ProjectID), SectionID: strings.TrimSpace(selection.SectionID),
		AssigneeID: strings.TrimSpace(input.AssigneeID), DueOn: strings.TrimSpace(input.DueOn),
	}
	if request.ProjectID == "" {
		request.ProjectID = strings.TrimSpace(input.ProjectID)
		request.SectionID = strings.TrimSpace(input.SectionID)
	}
	title, approvedBy := strings.TrimSpace(input.Title), strings.TrimSpace(input.ApprovedBy)
	switch {
	case !isRequestID(request.RequestID):
		return TaskRequest{}, errors.New("requestId must be 1 to 64 letters, digits, dashes, or underscores, such as REQ-1042")
	case title == "" || strings.ContainsAny(title, "\r\n"):
		return TaskRequest{}, errors.New("title is required and must be one line")
	case approvedBy == "" || strings.ContainsAny(approvedBy, "\r\n"):
		return TaskRequest{}, errors.New("approvedBy is required and must be one line")
	case !isTaskGID(request.ProjectID):
		return TaskRequest{}, errors.New("pick a project on the CreateRequestTask Step or set projectId to the project's gid")
	case request.SectionID != "" && !isTaskGID(request.SectionID):
		return TaskRequest{}, errors.New("sectionId must be the section's gid")
	case request.AssigneeID == "":
		return TaskRequest{}, errors.New("assigneeId is required: me, a user gid, or a workspace member's email address")
	}
	request.TaskName = TaskNamePrefix(request.RequestID) + " " + title
	request.Comment = "Approved by " + approvedBy + "."
	if note := strings.TrimSpace(input.ApprovalNote); note != "" {
		request.Comment += "\n\n" + note
	}
	return request, nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Reuse an open task that carries the request ID, read the next page, or create the task."
type evaluateOpenRequestTasks struct {
	dex.StepDefaultsNoWaitFor[asana.ListTasksResult]
}

func (evaluateOpenRequestTasks) GetStepType() string { return evaluateOpenRequestTasksStepType }

func (evaluateOpenRequestTasks) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestTaskPhaseAttribute), dex.LockAttribute(requestTaskAttribute)}}
}

func (evaluateOpenRequestTasks) Execute(ctx dex.Context, result asana.ListTasksResult) (*dex.StepDecision, error) {
	record, err := requestTaskAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.OpenTaskPagesRead++
	if task, isFound := FindRequestTask(result.Value.Tasks, record.Request.RequestID); isFound {
		record.Phase, record.IsExistingTaskReused, record.TaskID = PhaseAssigning, true, task.ID
		if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := requestTaskAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[TaskAssignment](assignRequestTaskStepType), newTaskAssignment(record)), nil
	}
	if result.Value.NextOffset != "" {
		if record.OpenTaskPagesRead >= MaximumOpenTaskPages {
			record.Phase = PhaseDuplicateCheckIncomplete
			if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
				return nil, err
			}
			if err := requestTaskAttribute.Set(ctx, record); err != nil {
				return nil, err
			}
			return dex.GracefulComplete(record), nil
		}
		if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := requestTaskAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[OpenTaskPage](findOpenRequestTasksStepType),
			OpenTaskPage{ProjectID: record.Request.ProjectID, Offset: result.Value.NextOffset}), nil
	}
	record.Phase = PhaseCreating
	record.CreateAttempts = 1
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TaskRequest](createRequestTaskStepType), record.Request), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Record the created task ID and assign the task."
type recordCreatedTask struct {
	dex.StepDefaultsNoWaitFor[asana.CreateTaskResult]
}

func (recordCreatedTask) GetStepType() string { return recordCreatedTaskStepType }

func (recordCreatedTask) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestTaskPhaseAttribute), dex.LockAttribute(requestTaskAttribute)}}
}

func (recordCreatedTask) Execute(ctx dex.Context, result asana.CreateTaskResult) (*dex.StepDecision, error) {
	record, err := requestTaskAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.TaskID = PhaseAssigning, result.Value.TaskID
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TaskAssignment](assignRequestTaskStepType), newTaskAssignment(record)), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Complete as rejected after Asana conclusively refused the create."
type recordRejectedTask struct {
	dex.StepDefaultsNoWaitFor[asana.CreateTaskResult]
}

func (recordRejectedTask) GetStepType() string { return recordRejectedTaskStepType }

func (recordRejectedTask) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestTaskPhaseAttribute), dex.LockAttribute(requestTaskAttribute)}}
}

func (recordRejectedTask) Execute(ctx dex.Context, _ asana.CreateTaskResult) (*dex.StepDecision, error) {
	record, err := requestTaskAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseRejected
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Park an unknown create for an operator instead of creating the task again."
type recordUncertainTask struct {
	dex.StepDefaultsNoWaitFor[asana.CreateTaskResult]
}

func (recordUncertainTask) GetStepType() string { return recordUncertainTaskStepType }

func (recordUncertainTask) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestTaskPhaseAttribute), dex.LockAttribute(requestTaskAttribute)}}
}

func (recordUncertainTask) Execute(ctx dex.Context, result asana.CreateTaskResult) (*dex.StepDecision, error) {
	record, err := requestTaskAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseNeedsReconciliation
	record.UncertainCreate = &UncertainCreate{CallID: string(result.Receipt.CallID), ObservedAt: result.Receipt.ObservedAt}
	if result.Failure != nil {
		record.UncertainCreate.FailureKind = result.Failure.Kind
	}
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Adopt the reported task when it is in the project and carries the request ID, or return it to the operator."
type verifyReportedTask struct {
	dex.StepDefaultsNoWaitFor[asana.GetTaskResult]
}

func (verifyReportedTask) GetStepType() string { return verifyReportedTaskStepType }

func (verifyReportedTask) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestTaskPhaseAttribute), dex.LockAttribute(requestTaskAttribute)}}
}

func (verifyReportedTask) Execute(ctx dex.Context, result asana.GetTaskResult) (*dex.StepDecision, error) {
	record, err := requestTaskAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	task := result.Value
	if !task.IsInProject(record.Request.ProjectID) || !strings.HasPrefix(strings.TrimSpace(task.Name), TaskNamePrefix(record.Request.RequestID)) {
		record.Phase, record.ReconciliationNote = PhaseNeedsReconciliation, NoteReportedTaskMismatch
		if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
			return nil, err
		}
		if err := requestTaskAttribute.Set(ctx, record); err != nil {
			return nil, err
		}
		return dex.DeadEnd(), nil
	}
	record.Phase, record.TaskID, record.UncertainCreate = PhaseAssigning, task.ID, nil
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TaskAssignment](assignRequestTaskStepType), newTaskAssignment(record)), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Return a reported task ID that Asana cannot find to the operator."
type recordMissingReportedTask struct {
	dex.StepDefaultsNoWaitFor[asana.GetTaskResult]
}

func (recordMissingReportedTask) GetStepType() string { return recordMissingReportedTaskStepType }

func (recordMissingReportedTask) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestTaskPhaseAttribute), dex.LockAttribute(requestTaskAttribute)}}
}

func (recordMissingReportedTask) Execute(ctx dex.Context, _ asana.GetTaskResult) (*dex.StepDecision, error) {
	record, err := requestTaskAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.ReconciliationNote = PhaseNeedsReconciliation, NoteReportedTaskMissing
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Record the assigned task read back and add the approval comment."
type recordAssignedTask struct {
	dex.StepDefaultsNoWaitFor[asana.UpdateTaskResult]
}

func (recordAssignedTask) GetStepType() string { return recordAssignedTaskStepType }

func (recordAssignedTask) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestTaskPhaseAttribute), dex.LockAttribute(requestTaskAttribute)}}
}

func (recordAssignedTask) Execute(ctx dex.Context, result asana.UpdateTaskResult) (*dex.StepDecision, error) {
	record, err := requestTaskAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.Task = PhaseCommenting, result.Value.Task
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CommentRequest](commentOnRequestTaskStepType), CommentRequest{TaskID: record.TaskID, Text: record.Request.Comment}), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Complete with the existing task when Asana rejected its assignee, due date, or section."
type recordRejectedAssignment struct {
	dex.StepDefaultsNoWaitFor[asana.UpdateTaskResult]
}

func (recordRejectedAssignment) GetStepType() string { return recordRejectedAssignmentStepType }

func (recordRejectedAssignment) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestTaskPhaseAttribute), dex.LockAttribute(requestTaskAttribute)}}
}

func (recordRejectedAssignment) Execute(ctx dex.Context, _ asana.UpdateTaskResult) (*dex.StepDecision, error) {
	record, err := requestTaskAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseAssignmentRejected
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Record the approval comment ID, or that its outcome is unknown, and complete without re-sending."
type recordApprovalComment struct {
	dex.StepDefaultsNoWaitFor[asana.AddCommentResult]
}

func (recordApprovalComment) GetStepType() string { return recordApprovalCommentStepType }

func (recordApprovalComment) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(requestTaskPhaseAttribute), dex.LockAttribute(requestTaskAttribute)}}
}

func (recordApprovalComment) Execute(ctx dex.Context, result asana.AddCommentResult) (*dex.StepDecision, error) {
	record, err := requestTaskAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase = PhaseReady
	record.CommentID = result.Value.CommentID
	record.IsCommentOutcomeUnknown = result.Branch == asana.AddCommentBranchUncertain
	if err := requestTaskPhaseAttribute.Set(ctx, record.Phase); err != nil {
		return nil, err
	}
	if err := requestTaskAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

func newTaskAssignment(record RequestTask) TaskAssignment {
	return TaskAssignment{TaskID: record.TaskID, AssigneeID: record.Request.AssigneeID, DueOn: record.Request.DueOn, SectionID: record.Request.SectionID}
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

func isTaskGID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
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
var _ dex.RPC[ConfirmCreatedTaskInput, dex.None] = (*Flow)(nil).ConfirmCreatedTask
var _ dex.RPC[dex.None, dex.None] = (*Flow)(nil).ApproveTaskCreateRetry
var _ dex.RPC[dex.None, RequestTask] = (*Flow)(nil).GetRequestTask
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
