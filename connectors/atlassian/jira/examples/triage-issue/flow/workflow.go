// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package triageissue demonstrates every Jira operation in one Flow started from Dex Web Start Flow:
// search for an open issue with the same summary, create one only when none exists, read it back,
// comment on it, and move it to a destination status. An uncertain create is parked for an operator
// instead of being created again.
package triageissue

import (
	"errors"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "JiraIssueTriage"
	// ConnectionName is the static Dex Web connection for Jira.
	ConnectionName = "jira-triage"
	// ReconcileIssueTriagePermission is required by both reconciliation Actions.
	ReconcileIssueTriagePermission = "jira-issue-triage.reconcile"
	// DefaultIssueTypeName is used when the Start Flow input names no issue type.
	DefaultIssueTypeName = "Task"

	recordTriageRequestStepType         = "RecordTriageRequest"
	findOpenDuplicateIssuesStepType     = "FindOpenDuplicateIssues"
	evaluateDuplicateIssuesStepType     = "EvaluateDuplicateIssues"
	createTriageIssueStepType           = "CreateTriageIssue"
	recordCreatedIssueStepType          = "RecordCreatedIssue"
	recordRejectedIssueStepType         = "RecordRejectedIssue"
	recordUncertainIssueStepType        = "RecordUncertainIssue"
	readBackTriageIssueStepType         = "ReadBackTriageIssue"
	verifyTriageIssueStepType           = "VerifyTriageIssue"
	recordMissingTriageIssueStepType    = "RecordMissingTriageIssue"
	addTriageCommentStepType            = "AddTriageComment"
	recordTriageCommentStepType         = "RecordTriageComment"
	moveTriageIssueStepType             = "MoveTriageIssue"
	recordUnavailableTransitionStepType = "RecordUnavailableTransition"
	completeIssueTriageStepType         = "CompleteIssueTriage"

	maximumDuplicateCandidates = 20
)

// Triage phases stored in the jira-issue-triage-phase Attribute.
const (
	// PhaseSearching means the Flow is looking for an open issue with the same summary.
	PhaseSearching = "searching"
	// PhaseCreating means a createIssue Step is about to run or running.
	PhaseCreating = "creating"
	// PhaseVerifyingIssue means the Flow is reading back the created or reused issue.
	PhaseVerifyingIssue = "verifyingIssue"
	// PhaseNeedsReconciliation means the create outcome is unknown and an operator must confirm or retry.
	PhaseNeedsReconciliation = "needsReconciliation"
	// PhaseVerifyingReportedIssue means the Flow is reading the issue key an operator reported.
	PhaseVerifyingReportedIssue = "verifyingReportedIssue"
	// PhaseCommenting means the Flow is adding the triage comment.
	PhaseCommenting = "commenting"
	// PhaseTransitioning means the Flow is moving the issue to the destination status.
	PhaseTransitioning = "transitioning"
	// PhaseTriaged means the issue exists, carries the comment, and is in the destination status.
	PhaseTriaged = "triaged"
	// PhaseTransitionUnavailable means the issue's workflow offers no transition to the destination status.
	PhaseTransitionUnavailable = "transitionUnavailable"
	// PhaseRejected means Jira conclusively rejected the create and nothing was created.
	PhaseRejected = "rejected"
)

// Reconciliation notes explain why a reported issue key was not adopted.
const (
	// NoteReportedIssueMissing means Jira has no visible issue with the reported key.
	NoteReportedIssueMissing = "Jira has no visible issue with the reported key"
	// NoteReportedIssueMismatch means the reported issue is in another project or has another summary.
	NoteReportedIssueMismatch = "the reported issue is in another project or has another summary"
)

var (
	triagePhaseAttribute = dex.DefineAttribute[string]("jira-issue-triage-phase")
	triageAttribute      = dex.DefineAttribute[IssueTriage]("jira-issue-triage")
)

var errReportedIssueKeyInvalid = errors.New("issueKey must be an issue key such as OPS-442")

// Input is the triage request entered in Dex Web Start Flow.
type Input struct {
	// Summary is the issue title, at most 255 characters; an open issue with the same summary is reused.
	Summary string `json:"summary"`
	// Description is the plain-text issue description used when a new issue is created.
	Description string `json:"description,omitempty"`
	// ProjectKey names the project, such as OPS, when no project was picked in Dex Web.
	ProjectKey string `json:"projectKey,omitempty"`
	// IssueTypeName is the issue type for a new issue; blank uses Task.
	IssueTypeName string `json:"issueTypeName,omitempty"`
	// Labels lists labels for a new issue, without whitespace.
	Labels []string `json:"labels,omitempty"`
	// AssigneeAccountID assigns a new issue to this Atlassian account ID.
	AssigneeAccountID string `json:"assigneeAccountId,omitempty"`
	// TriageComment is added to the issue as a plain-text comment; blank adds none.
	TriageComment string `json:"triageComment,omitempty"`
	// DestinationStatusName moves the issue to this status, such as In Progress; blank leaves it.
	DestinationStatusName string `json:"destinationStatusName,omitempty"`
}

// ProjectSelection is the value the project picker saves for the CreateTriageIssue Step.
type ProjectSelection struct {
	// ProjectID is the picked project's numeric ID.
	ProjectID string `json:"projectId,omitempty"`
	// ProjectKey is the picked project's key; blank uses the Start Flow projectKey.
	ProjectKey string `json:"projectKey,omitempty"`
	// ProjectName is the picked project's display name.
	ProjectName string `json:"projectName,omitempty"`
}

// TriageRequest is the validated request every later Step reads.
type TriageRequest struct {
	// ProjectKey is the project the issue belongs to.
	ProjectKey string `json:"projectKey"`
	// Summary is the trimmed issue title.
	Summary string `json:"summary"`
	// Description is the plain-text issue description.
	Description string `json:"description,omitempty"`
	// IssueTypeName is the issue type for a new issue.
	IssueTypeName string `json:"issueTypeName"`
	// Labels lists labels for a new issue.
	Labels []string `json:"labels,omitempty"`
	// AssigneeAccountID assigns a new issue.
	AssigneeAccountID string `json:"assigneeAccountId,omitempty"`
	// TriageComment is the comment to add; blank adds none.
	TriageComment string `json:"triageComment,omitempty"`
	// DestinationStatusName is the status to move the issue to; blank leaves it.
	DestinationStatusName string `json:"destinationStatusName,omitempty"`
}

// IssueReference identifies the issue the next read-back Step reads.
type IssueReference struct {
	// IssueKey is the issue key, such as OPS-442.
	IssueKey string `json:"issueKey"`
}

// CommentRequest is one comment for the addComment Step.
type CommentRequest struct {
	// IssueKey is the issue to comment on.
	IssueKey string `json:"issueKey"`
	// Body is the plain-text comment.
	Body string `json:"body"`
}

// StatusChangeRequest is one move for the transitionIssue Step.
type StatusChangeRequest struct {
	// IssueKey is the issue to move.
	IssueKey string `json:"issueKey"`
	// DestinationStatusName is the status to move it to.
	DestinationStatusName string `json:"destinationStatusName"`
}

// UncertainCreate records a dispatched create whose outcome Jira did not confirm.
type UncertainCreate struct {
	// CallID is the connector call identity of the uncertain create.
	CallID string `json:"callId"`
	// ObservedAt is when the connector observed the unknown outcome; search the project around it.
	ObservedAt time.Time `json:"observedAt"`
	// FailureKind is the safe connector failure category, such as TRANSPORT.
	FailureKind sdkgo.FailureKind `json:"failureKind"`
}

// IssueTriage is the Flow's durable record of one triaged issue.
type IssueTriage struct {
	// Request is the validated request.
	Request TriageRequest `json:"request"`
	// Phase mirrors the jira-issue-triage-phase Attribute.
	Phase string `json:"phase"`
	// CreateAttempts counts createIssue Step executions: the first create plus each approved retry.
	CreateAttempts int `json:"createAttempts"`
	// IsExistingIssueReused reports that search found an open issue with the same summary.
	IsExistingIssueReused bool `json:"isExistingIssueReused,omitempty"`
	// Issue is the latest read-back of the triaged issue.
	Issue *jira.Issue `json:"issue,omitempty"`
	// CommentID is the triage comment's ID once Jira confirmed it.
	CommentID string `json:"commentId,omitempty"`
	// IsCommentOutcomeUnknown reports a comment Jira may or may not have stored; it is never re-sent.
	IsCommentOutcomeUnknown bool `json:"isCommentOutcomeUnknown,omitempty"`
	// Status is the issue's status after the transition Step.
	Status *jira.IssueStatus `json:"status,omitempty"`
	// AvailableTransitionNames lists the transitions Jira offered when none reached the destination.
	AvailableTransitionNames []string `json:"availableTransitionNames,omitempty"`
	// RejectedFieldIDs lists the fields Jira named when it rejected the create.
	RejectedFieldIDs []string `json:"rejectedFieldIds,omitempty"`
	// UncertainCreate describes the latest uncertain create while reconciliation is pending.
	UncertainCreate *UncertainCreate `json:"uncertainCreate,omitempty"`
	// ReconciliationNote explains why a reported issue key was not adopted.
	ReconciliationNote string `json:"reconciliationNote,omitempty"`
}

// ConfirmCreatedIssueInput reports the issue an operator found after an uncertain create.
type ConfirmCreatedIssueInput struct {
	// IssueKey is the key of the issue found in the project, such as OPS-442.
	IssueKey string `json:"issueKey"`
}

// Flow triages one Jira issue.
type Flow struct {
	dex.FlowDefaults
	connection jira.Connection
	selection  ProjectSelection
}

// NewFlow binds the Jira Connection and the picked project at registration time.
// A blank selection uses each Start Flow input's projectKey.
func NewFlow(connection jira.Connection, selection ProjectSelection) *Flow {
	selection.ProjectKey = strings.TrimSpace(selection.ProjectKey)
	return &Flow{connection: connection, selection: selection}
}

// ProjectSelectionConfigurationRef identifies the project picker value saved in Dex Web.
func ProjectSelectionConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: jira.ConnectorID, ConnectionName: ConnectionName, OperationID: "createIssue",
		FlowType: FlowType, StepType: createTriageIssueStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Jira connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordTriageRequest{projectKey: flow.selection.ProjectKey}),
		dex.DefineStep(jira.NewSearchIssuesStep(jira.SearchIssuesStepConfig[TriageRequest]{
			StepType: findOpenDuplicateIssuesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "jira", GroupLabel: "Jira",
				Explanation: "Search the project for open issues whose summary contains the requested summary.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSearchIssuesInput,
			Searched: sdkgo.GoTo(evaluateDuplicateIssues{}),
		})),
		dex.DefineStep(evaluateDuplicateIssues{}),
		dex.DefineStep(jira.NewCreateIssueStep(jira.CreateIssueStepConfig[TriageRequest]{
			StepType: createTriageIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "jira", GroupLabel: "Jira",
				Explanation: "Create the issue once; an unknown outcome is reconciled, never created again automatically.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "triageProject", UnitID: jira.UIUnitProjectPicker, Label: "Triage project",
				Description: "Choose the Jira project that receives triage issues; the list shows only projects this connection can create issues in, and the duplicate search, comment, and transition Steps use the same project. Leave it unsaved to use each Start Flow input's projectKey. Restart the Worker after saving.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: jira.UIProjectPickerPortProjectID, JSONPointer: "/projectId"},
					{Port: jira.UIProjectPickerPortProjectKey, JSONPointer: "/projectKey"},
					{Port: jira.UIProjectPickerPortProjectName, JSONPointer: "/projectName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: MapToCreateIssueInput,
			Created:          sdkgo.GoTo(recordCreatedIssue{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedIssue{}),
			Uncertain:        sdkgo.GoTo(recordUncertainIssue{}),
		})),
		dex.DefineStep(recordCreatedIssue{}),
		dex.DefineStep(recordRejectedIssue{}),
		dex.DefineStep(recordUncertainIssue{}),
		dex.DefineStep(jira.NewGetIssueStep(jira.GetIssueStepConfig[IssueReference]{
			StepType: readBackTriageIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "jira", GroupLabel: "Jira",
				Explanation: "Read the issue back from Jira to confirm its project, summary, and status.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetIssueInput,
			Found:    sdkgo.GoTo(verifyTriageIssue{}),
			NotFound: sdkgo.GoTo(recordMissingTriageIssue{}),
		})),
		dex.DefineStep(verifyTriageIssue{}),
		dex.DefineStep(recordMissingTriageIssue{}),
		dex.DefineStep(jira.NewAddCommentStep(jira.AddCommentStepConfig[CommentRequest]{
			StepType: addTriageCommentStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "jira", GroupLabel: "Jira",
				Explanation: "Add the triage comment once; an unknown outcome is recorded, never re-sent.",
			},
			Connection: flow.connection, MapToOperationInput: MapToAddCommentInput,
			Added:     sdkgo.GoTo(recordTriageComment{}),
			Uncertain: sdkgo.GoTo(recordTriageComment{}),
		})),
		dex.DefineStep(recordTriageComment{}),
		dex.DefineStep(jira.NewTransitionIssueStep(jira.TransitionIssueStepConfig[StatusChangeRequest]{
			StepType: moveTriageIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "jira", GroupLabel: "Jira",
				Explanation: "Move the issue to the destination status, skipping a move Jira already made.",
			},
			Connection: flow.connection, MapToOperationInput: MapToTransitionIssueInput,
			Transitioned:          sdkgo.GoTo(completeIssueTriage{}),
			TransitionUnavailable: sdkgo.GoTo(recordUnavailableTransition{}),
		})),
		dex.DefineStep(recordUnavailableTransition{}),
		dex.DefineStep(completeIssueTriage{}),
	}
}

// GetRPCs returns the reconciliation Actions, the triage read RPC, and the Dex Web views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	reconciliationLocks := []dex.AttributeLock{dex.LockAttribute(triagePhaseAttribute), dex.LockAttribute(triageAttribute)}
	return []dex.RPCDef{
		dex.DefineRPC(flow.ConfirmCreatedIssue, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Confirm created issue",
				dex.WhenAttributeMatches(triagePhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsReconciliation)),
				dex.ActionRequiresPermission(ReconcileIssueTriagePermission),
			),
			LockAttributes: reconciliationLocks,
		}),
		dex.DefineRPC(flow.ApproveIssueCreateRetry, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Create issue again",
				dex.WhenAttributeMatches(triagePhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsReconciliation)),
				dex.ActionRequiresPermission(ReconcileIssueTriagePermission),
			),
			LockAttributes: reconciliationLocks,
		}),
		dex.DefineRPC(flow.GetIssueTriage, nil),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the phase and triage Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{triagePhaseAttribute, triageAttribute}}
}

// MapToSearchIssuesInput searches the project's open issues with the summary as a phrase.
func MapToSearchIssuesInput(request TriageRequest) jira.SearchIssuesInput {
	return jira.SearchIssuesInput{
		Filter: &jira.IssueSearchFilter{
			ProjectKeys:      []string{request.ProjectKey},
			StatusCategories: []jira.StatusCategoryKey{jira.StatusCategoryToDo, jira.StatusCategoryInProgress},
			SummaryPhrase:    request.Summary,
		},
		PageSize: maximumDuplicateCandidates,
	}
}

// MapToCreateIssueInput maps the recorded request to one new issue.
func MapToCreateIssueInput(request TriageRequest) jira.CreateIssueInput {
	return jira.CreateIssueInput{
		ProjectKey: request.ProjectKey, IssueTypeName: request.IssueTypeName, Summary: request.Summary,
		Description: request.Description, Labels: request.Labels, AssigneeAccountID: request.AssigneeAccountID,
	}
}

// MapToGetIssueInput reads back one issue with its description.
func MapToGetIssueInput(reference IssueReference) jira.GetIssueInput {
	return jira.GetIssueInput{IssueIDOrKey: reference.IssueKey}
}

// MapToAddCommentInput maps the triage comment to one Jira comment.
func MapToAddCommentInput(request CommentRequest) jira.AddCommentInput {
	return jira.AddCommentInput{IssueIDOrKey: request.IssueKey, Body: request.Body}
}

// MapToTransitionIssueInput moves the issue by destination status, so a retried Step recognizes a move.
func MapToTransitionIssueInput(request StatusChangeRequest) jira.TransitionIssueInput {
	return jira.TransitionIssueInput{IssueIDOrKey: request.IssueKey, DestinationStatusName: request.DestinationStatusName}
}

// FindSameSummaryIssue returns the first open issue whose summary equals summary without case or
// surrounding whitespace. Jira text search is partial, so near-duplicates are rejected here.
func FindSameSummaryIssue(issues []jira.Issue, summary string) (jira.Issue, bool) {
	for _, issue := range issues {
		if issue.Status.CategoryKey != jira.StatusCategoryDone && strings.EqualFold(strings.TrimSpace(issue.Summary), strings.TrimSpace(summary)) {
			return issue, true
		}
	}
	return jira.Issue{}, false
}

// ConfirmCreatedIssue adopts an issue an operator found in the project after an uncertain create.
// The Flow reads that key with getIssue and adopts it only when its project and summary match.
//
// dex:input field-name:issueKey value-type:string source:user required:true description:"Key of the issue found in the project, such as OPS-442"
func (*Flow) ConfirmCreatedIssue(ctx dex.Context, input ConfirmCreatedIssueInput) (*dex.RPCResult[dex.None], error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if triage.Phase != PhaseNeedsReconciliation {
		return &dex.RPCResult[dex.None]{}, nil
	}
	issueKey := strings.ToUpper(strings.TrimSpace(input.IssueKey))
	if !isIssueKey(issueKey) {
		return nil, errReportedIssueKeyInvalid
	}
	triage.Phase = PhaseVerifyingReportedIssue
	triage.ReconciliationNote = ""
	if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
		return nil, err
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[IssueReference](readBackTriageIssueStepType), IssueReference{IssueKey: issueKey})},
	}, nil
}

// ApproveIssueCreateRetry creates the issue again after an operator confirmed the project has no such
// issue. The retry is a new Step execution with a new connector call ID.
func (*Flow) ApproveIssueCreateRetry(ctx dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if triage.Phase != PhaseNeedsReconciliation {
		return &dex.RPCResult[dex.None]{}, nil
	}
	triage.Phase = PhaseCreating
	triage.CreateAttempts++
	triage.UncertainCreate = nil
	triage.ReconciliationNote = ""
	if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
		return nil, err
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[TriageRequest](createTriageIssueStepType), triage.Request)},
	}, nil
}

// GetIssueTriage returns the current triage record.
func (*Flow) GetIssueTriage(ctx dex.Context, _ dex.None) (*dex.RPCResult[IssueTriage], error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[IssueTriage]{Output: triage}, nil
}

// GetDexSummary returns the triage phase and record for Dex Web lists.
//
// dex:field attribute-key:jira-issue-triage-phase value-type:string editable:false description:"Triage phase"
// dex:field attribute-key:jira-issue-triage value-type:json editable:false description:"Project, summary, issue key, comment, and status"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, triagePhaseAttribute)
	if err != nil {
		return nil, err
	}
	triage, err := optionalAttribute(ctx, triageAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"jira-issue-triage-phase": phase,
		"jira-issue-triage":       triage,
	}}, nil
}

// GetDexDisplay returns the triage phase and record for the Dex Web run view.
//
// dex:field attribute-key:jira-issue-triage-phase value-type:string editable:false description:"Triage phase" ui-slot:status
// dex:field attribute-key:jira-issue-triage value-type:json editable:false description:"Request, read-back issue, comment, status, and any uncertain create awaiting reconciliation"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, triagePhaseAttribute)
	if err != nil {
		return nil, err
	}
	triage, err := optionalAttribute(ctx, triageAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"jira-issue-triage-phase": phase,
		"jira-issue-triage":       triage,
	}}, nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Validate the request and record it with the picked project before calling Jira."
type recordTriageRequest struct {
	dex.StepDefaults
	projectKey string
}

func (recordTriageRequest) GetStepType() string { return recordTriageRequestStepType }

func (recordTriageRequest) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triagePhaseAttribute), dex.LockAttribute(triageAttribute)}}
}

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordTriageRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordTriageRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildTriageRequest(step.projectKey, input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	triage := IssueTriage{Request: request, Phase: PhaseSearching}
	if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
		return nil, err
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageRequest](findOpenDuplicateIssuesStepType), request), nil
}

// BuildTriageRequest validates Start Flow input. A picked project key wins over the input's projectKey.
func BuildTriageRequest(pickedProjectKey string, input Input) (TriageRequest, error) {
	request := TriageRequest{
		ProjectKey: strings.TrimSpace(pickedProjectKey), Summary: strings.TrimSpace(input.Summary),
		Description: input.Description, IssueTypeName: strings.TrimSpace(input.IssueTypeName), Labels: input.Labels,
		AssigneeAccountID: strings.TrimSpace(input.AssigneeAccountID), TriageComment: strings.TrimSpace(input.TriageComment),
		DestinationStatusName: strings.TrimSpace(input.DestinationStatusName),
	}
	if request.ProjectKey == "" {
		request.ProjectKey = strings.TrimSpace(input.ProjectKey)
	}
	if request.ProjectKey == "" {
		return TriageRequest{}, errors.New("pick a project on the CreateTriageIssue Step or set projectKey")
	}
	if request.Summary == "" {
		return TriageRequest{}, errors.New("summary is required")
	}
	if strings.ContainsAny(request.Summary, "\r\n") {
		return TriageRequest{}, errors.New("summary must be one line")
	}
	if request.IssueTypeName == "" {
		request.IssueTypeName = DefaultIssueTypeName
	}
	return request, nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Reuse an open issue with exactly the same summary, otherwise create a new one."
type evaluateDuplicateIssues struct {
	dex.StepDefaultsNoWaitFor[jira.SearchIssuesResult]
}

func (evaluateDuplicateIssues) GetStepType() string { return evaluateDuplicateIssuesStepType }

func (evaluateDuplicateIssues) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triagePhaseAttribute), dex.LockAttribute(triageAttribute)}}
}

func (evaluateDuplicateIssues) Execute(ctx dex.Context, result jira.SearchIssuesResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if issue, isFound := FindSameSummaryIssue(result.Value.Issues, triage.Request.Summary); isFound {
		triage.Phase = PhaseVerifyingIssue
		triage.IsExistingIssueReused = true
		if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
			return nil, err
		}
		if err := triageAttribute.Set(ctx, triage); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[IssueReference](readBackTriageIssueStepType), IssueReference{IssueKey: issue.Key}), nil
	}
	triage.Phase = PhaseCreating
	triage.CreateAttempts = 1
	if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
		return nil, err
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageRequest](createTriageIssueStepType), triage.Request), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the created issue key and read the issue back."
type recordCreatedIssue struct {
	dex.StepDefaultsNoWaitFor[jira.CreateIssueResult]
}

func (recordCreatedIssue) GetStepType() string { return recordCreatedIssueStepType }

func (recordCreatedIssue) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triagePhaseAttribute), dex.LockAttribute(triageAttribute)}}
}

func (recordCreatedIssue) Execute(ctx dex.Context, result jira.CreateIssueResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Phase = PhaseVerifyingIssue
	if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
		return nil, err
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[IssueReference](readBackTriageIssueStepType), IssueReference{IssueKey: result.Value.IssueKey}), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Complete with the rejected field IDs after Jira conclusively refused the create."
type recordRejectedIssue struct {
	dex.StepDefaultsNoWaitFor[jira.CreateIssueResult]
}

func (recordRejectedIssue) GetStepType() string { return recordRejectedIssueStepType }

func (recordRejectedIssue) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triagePhaseAttribute), dex.LockAttribute(triageAttribute)}}
}

func (recordRejectedIssue) Execute(ctx dex.Context, result jira.CreateIssueResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Phase = PhaseRejected
	triage.RejectedFieldIDs = result.Value.RejectedFieldIDs
	if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
		return nil, err
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(triage), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Park an unknown create for an operator instead of creating the issue again."
type recordUncertainIssue struct {
	dex.StepDefaultsNoWaitFor[jira.CreateIssueResult]
}

func (recordUncertainIssue) GetStepType() string { return recordUncertainIssueStepType }

func (recordUncertainIssue) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triagePhaseAttribute), dex.LockAttribute(triageAttribute)}}
}

func (recordUncertainIssue) Execute(ctx dex.Context, result jira.CreateIssueResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Phase = PhaseNeedsReconciliation
	triage.UncertainCreate = &UncertainCreate{CallID: string(result.Receipt.CallID), ObservedAt: result.Receipt.ObservedAt}
	if result.Failure != nil {
		triage.UncertainCreate.FailureKind = result.Failure.Kind
	}
	if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
		return nil, err
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Adopt the read-back issue, or return a mismatched reported issue to the operator."
type verifyTriageIssue struct {
	dex.StepDefaultsNoWaitFor[jira.GetIssueResult]
}

func (verifyTriageIssue) GetStepType() string { return verifyTriageIssueStepType }

func (verifyTriageIssue) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triagePhaseAttribute), dex.LockAttribute(triageAttribute)}}
}

func (verifyTriageIssue) Execute(ctx dex.Context, result jira.GetIssueResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	issue := result.Value
	isSameIssue := issue.Project.Key == triage.Request.ProjectKey && strings.EqualFold(strings.TrimSpace(issue.Summary), triage.Request.Summary)
	if triage.Phase == PhaseVerifyingReportedIssue && !isSameIssue {
		triage.Phase = PhaseNeedsReconciliation
		triage.ReconciliationNote = NoteReportedIssueMismatch
		if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
			return nil, err
		}
		if err := triageAttribute.Set(ctx, triage); err != nil {
			return nil, err
		}
		return dex.DeadEnd(), nil
	}
	triage.Issue = &issue
	triage.UncertainCreate = nil
	switch {
	case triage.Request.TriageComment != "":
		triage.Phase = PhaseCommenting
		if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
			return nil, err
		}
		if err := triageAttribute.Set(ctx, triage); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[CommentRequest](addTriageCommentStepType), CommentRequest{IssueKey: issue.Key, Body: triage.Request.TriageComment}), nil
	case triage.Request.DestinationStatusName != "":
		triage.Phase = PhaseTransitioning
		if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
			return nil, err
		}
		if err := triageAttribute.Set(ctx, triage); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[StatusChangeRequest](moveTriageIssueStepType), newStatusChangeRequest(triage)), nil
	default:
		triage.Phase, triage.Status = PhaseTriaged, &issue.Status
		if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
			return nil, err
		}
		if err := triageAttribute.Set(ctx, triage); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(triage), nil
	}
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Return a reported issue key Jira cannot find to the operator."
type recordMissingTriageIssue struct {
	dex.StepDefaultsNoWaitFor[jira.GetIssueResult]
}

func (recordMissingTriageIssue) GetStepType() string { return recordMissingTriageIssueStepType }

func (recordMissingTriageIssue) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triagePhaseAttribute), dex.LockAttribute(triageAttribute)}}
}

func (recordMissingTriageIssue) Execute(ctx dex.Context, _ jira.GetIssueResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if triage.Phase != PhaseVerifyingReportedIssue {
		return dex.ForceFail("the triage issue can no longer be read from Jira"), nil
	}
	triage.Phase = PhaseNeedsReconciliation
	triage.ReconciliationNote = NoteReportedIssueMissing
	if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
		return nil, err
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the comment ID, or that its outcome is unknown, then continue without re-sending."
type recordTriageComment struct {
	dex.StepDefaultsNoWaitFor[jira.AddCommentResult]
}

func (recordTriageComment) GetStepType() string { return recordTriageCommentStepType }

func (recordTriageComment) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triagePhaseAttribute), dex.LockAttribute(triageAttribute)}}
}

func (recordTriageComment) Execute(ctx dex.Context, result jira.AddCommentResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.CommentID = result.Value.CommentID
	triage.IsCommentOutcomeUnknown = result.Branch == jira.AddCommentBranchUncertain
	if triage.Request.DestinationStatusName != "" {
		triage.Phase = PhaseTransitioning
		if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
			return nil, err
		}
		if err := triageAttribute.Set(ctx, triage); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[StatusChangeRequest](moveTriageIssueStepType), newStatusChangeRequest(triage)), nil
	}
	triage.Phase, triage.Status = PhaseTriaged, &triage.Issue.Status
	if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
		return nil, err
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(triage), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Complete with the offered transitions when none reaches the destination status."
type recordUnavailableTransition struct {
	dex.StepDefaultsNoWaitFor[jira.TransitionIssueResult]
}

func (recordUnavailableTransition) GetStepType() string { return recordUnavailableTransitionStepType }

func (recordUnavailableTransition) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triagePhaseAttribute), dex.LockAttribute(triageAttribute)}}
}

func (recordUnavailableTransition) Execute(ctx dex.Context, result jira.TransitionIssueResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Phase = PhaseTransitionUnavailable
	status := result.Value.Status
	triage.Status = &status
	triage.AvailableTransitionNames = nil
	for _, transition := range result.Value.AvailableTransitions {
		triage.AvailableTransitionNames = append(triage.AvailableTransitionNames, transition.Name+" -> "+transition.DestinationStatus.Name)
	}
	if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
		return nil, err
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(triage), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the issue's destination status and complete the triage."
type completeIssueTriage struct {
	dex.StepDefaultsNoWaitFor[jira.TransitionIssueResult]
}

func (completeIssueTriage) GetStepType() string { return completeIssueTriageStepType }

func (completeIssueTriage) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(triagePhaseAttribute), dex.LockAttribute(triageAttribute)}}
}

func (completeIssueTriage) Execute(ctx dex.Context, result jira.TransitionIssueResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	status := result.Value.Status
	triage.Status = &status
	triage.Phase = PhaseTriaged
	if err := triagePhaseAttribute.Set(ctx, triage.Phase); err != nil {
		return nil, err
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(triage), nil
}

func newStatusChangeRequest(triage IssueTriage) StatusChangeRequest {
	return StatusChangeRequest{IssueKey: triage.Issue.Key, DestinationStatusName: triage.Request.DestinationStatusName}
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

func isIssueKey(value string) bool {
	projectKey, number, hasSeparator := strings.Cut(value, "-")
	if !hasSeparator || projectKey == "" || number == "" || number[0] == '0' || projectKey[0] < 'A' || projectKey[0] > 'Z' {
		return false
	}
	for _, character := range projectKey {
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	for _, character := range number {
		if character < '0' || character > '9' {
			return false
		}
	}
	return len(number) <= 18
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[ConfirmCreatedIssueInput, dex.None] = (*Flow)(nil).ConfirmCreatedIssue
var _ dex.RPC[dex.None, dex.None] = (*Flow)(nil).ApproveIssueCreateRetry
var _ dex.RPC[dex.None, IssueTriage] = (*Flow)(nil).GetIssueTriage
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
