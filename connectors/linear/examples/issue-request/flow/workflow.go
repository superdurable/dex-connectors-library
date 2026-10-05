// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package issuerequest demonstrates every Linear operation in one Flow started from Dex Web Start Flow:
// resolve the assignee by email, reuse the team's open issue with the requested title or create one,
// move it to the requested workflow state, add one comment, and read the issue back.
package issuerequest

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "LinearIssueRequest"
	// ConnectionName is the static Dex Web connection for Linear.
	ConnectionName = "linear-workspace"
	// DefaultComment is the comment the Flow adds when the request names none.
	DefaultComment = "Filed by Dex from an issue request."

	recordIssueRequestStepType   = "RecordIssueRequest"
	findAssigneeStepType         = "FindAssignee"
	recordAssigneeStepType       = "RecordAssignee"
	findOpenIssueStepType        = "FindOpenIssue"
	chooseOpenIssueStepType      = "ChooseOpenIssue"
	createRequestedIssueStepType = "CreateRequestedIssue"
	recordCreatedIssueStepType   = "RecordCreatedIssue"
	recordRejectedIssueStepType  = "RecordRejectedIssue"
	listTeamStatesStepType       = "ListTeamStates"
	chooseTargetStateStepType    = "ChooseTargetState"
	moveIssueStepType            = "MoveIssue"
	recordMovedIssueStepType     = "RecordMovedIssue"
	addIssueCommentStepType      = "AddIssueComment"
	recordCommentStepType        = "RecordComment"
	readBackIssueStepType        = "ReadBackIssue"
	completeIssueRequestStepType = "CompleteIssueRequest"

	openIssueSearchPageSize = 10
)

var (
	issueRequestAttribute  = dex.DefineAttribute[IssueRequest]("linear-issue-request")
	issueOutcomeAttribute  = dex.DefineAttribute[IssueRequestOutcome]("linear-issue-outcome")
	errInvalidIssueRequest = errors.New("invalid issue request")
	// openStateTypes are the workflow state types of an issue that is still being worked on.
	openStateTypes = []linear.WorkflowStateType{
		linear.WorkflowStateTypeTriage, linear.WorkflowStateTypeBacklog, linear.WorkflowStateTypeUnstarted, linear.WorkflowStateTypeStarted,
	}
)

// Input is the issue request entered in Dex Web Start Flow.
type Input struct {
	// TeamID is the Linear team UUID; it is used only while the CreateRequestedIssue Step's team picker is unsaved.
	TeamID string `json:"teamId,omitempty"`
	// Title is the issue title, such as Monthly Fire Drill Checklist - February.
	Title string `json:"title"`
	// Description is optional Markdown for a newly created issue.
	Description string `json:"description,omitempty"`
	// AssigneeEmail is the email of the Linear user to assign; blank leaves the assignee unchanged.
	AssigneeEmail string `json:"assigneeEmail,omitempty"`
	// StateName is the team's workflow state to move the issue to, such as Todo; blank uses the team's first unstarted state.
	StateName string `json:"stateName,omitempty"`
	// Comment is the Markdown comment to add; blank uses DefaultComment.
	Comment string `json:"comment,omitempty"`
}

// TeamSelection is the team picker value saved for the CreateRequestedIssue Step.
type TeamSelection struct {
	// TeamID is the picked team's UUID.
	TeamID string `json:"teamId"`
	// TeamKey is the picked team's key, such as ENG.
	TeamKey string `json:"teamKey,omitempty"`
	// TeamName is the picked team's name.
	TeamName string `json:"teamName,omitempty"`
}

// IssueRequest is the validated request every later Step reads.
type IssueRequest struct {
	// TeamID is the team UUID.
	TeamID string `json:"teamId"`
	// Title is the exact issue title.
	Title string `json:"title"`
	// Description is the Markdown description of a created issue.
	Description string `json:"description,omitempty"`
	// AssigneeEmail is the requested assignee's email, or blank.
	AssigneeEmail string `json:"assigneeEmail,omitempty"`
	// AssigneeID is the resolved assignee's user UUID, or blank.
	AssigneeID string `json:"assigneeId,omitempty"`
	// StateName is the requested workflow state name, or blank.
	StateName string `json:"stateName,omitempty"`
	// Comment is the comment's Markdown.
	Comment string `json:"comment"`
}

// IssueChange moves the issue to the chosen state and assigns it.
type IssueChange struct {
	// IssueID is the issue's UUID.
	IssueID string `json:"issueId"`
	// StateID is the chosen workflow state.
	StateID string `json:"stateId"`
	// AssigneeID is the resolved assignee, or blank to keep the current one.
	AssigneeID string `json:"assigneeId,omitempty"`
}

// IssueComment is the comment to add.
type IssueComment struct {
	// IssueID is the issue's UUID.
	IssueID string `json:"issueId"`
	// Body is the comment's Markdown.
	Body string `json:"body"`
}

// IssueReference names the issue to read back.
type IssueReference struct {
	// IssueID is the issue's UUID.
	IssueID string `json:"issueId"`
}

// IssueAction is what the Flow did to have an issue.
type IssueAction string

const (
	// IssueCreated means the team had no open issue with the title, so one was created.
	IssueCreated IssueAction = "created"
	// IssueReused means the team's open issue with the title was moved and commented instead.
	IssueReused IssueAction = "reused"
	// IssueRejected means Linear rejected the create; nothing was created.
	IssueRejected IssueAction = "rejected"
)

// IssueRequestOutcome is the Flow result and the value of its outcome Attribute.
type IssueRequestOutcome struct {
	// Action is what the Flow did to have an issue.
	Action IssueAction `json:"action,omitempty"`
	// IssueID is the issue's UUID.
	IssueID string `json:"issueId,omitempty"`
	// Identifier is the issue's identifier, such as ENG-8.
	Identifier string `json:"identifier,omitempty"`
	// URL is the issue's page in Linear.
	URL string `json:"url,omitempty"`
	// WasCreateReplayed reports that an earlier attempt of the create Step had already created the issue.
	WasCreateReplayed bool `json:"createReplayed,omitempty"`
	// AssigneeID is the resolved assignee, or blank.
	AssigneeID string `json:"assigneeId,omitempty"`
	// IsAssigneeUnknown reports that no workspace user has AssigneeEmail, so the assignee was left unchanged.
	IsAssigneeUnknown bool `json:"assigneeUnknown,omitempty"`
	// StateName is the workflow state the Flow moved the issue to.
	StateName string `json:"stateName,omitempty"`
	// CommentID is the comment the Flow added.
	CommentID string `json:"commentId,omitempty"`
	// ReadBackStateType is the issue's state type as read back at the end, such as unstarted.
	ReadBackStateType linear.WorkflowStateType `json:"readBackStateType,omitempty"`
	// RejectionDetail is the connector's credential-free explanation of a rejected create.
	RejectionDetail string `json:"rejectionDetail,omitempty"`
}

// Flow handles one issue request in one Linear team.
type Flow struct {
	dex.FlowDefaults
	connection linear.Connection
	selection  TeamSelection
}

// NewFlow binds the Linear Connection and the picked team at registration time. A blank selection uses
// each Start Flow input's teamId.
func NewFlow(connection linear.Connection, selection TeamSelection) *Flow {
	selection.TeamID = strings.ToLower(strings.TrimSpace(selection.TeamID))
	return &Flow{connection: connection, selection: selection}
}

// TeamSelectionConfigurationRef identifies the team picker value saved in Dex Web.
func TeamSelectionConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: linear.ConnectorID, ConnectionName: ConnectionName, OperationID: "createIssue",
		FlowType: FlowType, StepType: createRequestedIssueStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Linear connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordIssueRequest{teamID: flow.selection.TeamID}),
		dex.DefineStep(linear.NewFindUserByEmailStep(linear.FindUserByEmailStepConfig[IssueRequest]{
			StepType: findAssigneeStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "linear", GroupLabel: "Linear", Explanation: "Resolve the requested assignee's email to a Linear user."},
			Connection:  flow.connection, MapToOperationInput: MapToFindUserByEmailInput,
			Found: sdkgo.GoTo(recordAssignee{}), NotFound: sdkgo.GoTo(recordAssignee{}),
		})),
		dex.DefineStep(recordAssignee{}),
		dex.DefineStep(linear.NewSearchIssuesStep(linear.SearchIssuesStepConfig[IssueRequest]{
			StepType: findOpenIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "linear", GroupLabel: "Linear", Explanation: "List the team's open issues whose title is exactly the requested title."},
			Connection:  flow.connection, MapToOperationInput: MapToSearchIssuesInput,
			Searched: sdkgo.GoTo(chooseOpenIssue{}),
		})),
		dex.DefineStep(chooseOpenIssue{}),
		dex.DefineStep(linear.NewCreateIssueStep(linear.CreateIssueStepConfig[IssueRequest]{
			StepType: createRequestedIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "linear", GroupLabel: "Linear", Explanation: "Create the issue under the Step's client-supplied UUID, so a repeated attempt finds it instead of creating another."},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "requestTeam", UnitID: linear.UIUnitTeamPicker, Label: "Request team",
				Description: "Choose the Linear team that receives issue requests; the open-issue search, workflow states, and new issues all use it. An OAuth connection lists its teams; with a personal API key, paste the team UUID from Linear Settings > Teams > the team > Copy team ID. Leave it unsaved to use each Start Flow input's teamId, and restart the Worker after saving.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: linear.UITeamPickerPortTeamID, JSONPointer: "/teamId"},
					{Port: linear.UITeamPickerPortTeamKey, JSONPointer: "/teamKey"},
					{Port: linear.UITeamPickerPortTeamName, JSONPointer: "/teamName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: MapToCreateIssueInput,
			Created: sdkgo.GoTo(recordCreatedIssue{}), ProviderRejected: sdkgo.GoTo(recordRejectedIssue{}),
		})),
		dex.DefineStep(recordCreatedIssue{}),
		dex.DefineStep(recordRejectedIssue{}),
		dex.DefineStep(linear.NewListWorkflowStatesStep(linear.ListWorkflowStatesStepConfig[IssueRequest]{
			StepType: listTeamStatesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "linear", GroupLabel: "Linear", Explanation: "List the team's workflow states to find the requested one."},
			Connection:  flow.connection, MapToOperationInput: MapToListWorkflowStatesInput,
			Listed: sdkgo.GoTo(chooseTargetState{}),
		})),
		dex.DefineStep(chooseTargetState{}),
		dex.DefineStep(linear.NewUpdateIssueStep(linear.UpdateIssueStepConfig[IssueChange]{
			StepType: moveIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "linear", GroupLabel: "Linear", Explanation: "Move the issue to the chosen state and assign it with absolute values, so a repeated attempt changes nothing."},
			Connection:  flow.connection, MapToOperationInput: MapToUpdateIssueInput,
			Updated: sdkgo.GoTo(recordMovedIssue{}),
		})),
		dex.DefineStep(recordMovedIssue{}),
		dex.DefineStep(linear.NewAddCommentStep(linear.AddCommentStepConfig[IssueComment]{
			StepType: addIssueCommentStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "linear", GroupLabel: "Linear", Explanation: "Add one comment under the Step's client-supplied UUID."},
			Connection:  flow.connection, MapToOperationInput: MapToAddCommentInput,
			Added: sdkgo.GoTo(recordComment{}),
		})),
		dex.DefineStep(recordComment{}),
		dex.DefineStep(linear.NewGetIssueStep(linear.GetIssueStepConfig[IssueReference]{
			StepType: readBackIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "linear", GroupLabel: "Linear", Explanation: "Read the issue back to confirm its workflow state and assignee."},
			Connection:  flow.connection, MapToOperationInput: MapToGetIssueInput,
			Found: sdkgo.GoTo(completeIssueRequest{}),
		})),
		dex.DefineStep(completeIssueRequest{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{issueRequestAttribute, issueOutcomeAttribute}}
}

// GetDexSummary returns the issue request and its outcome.
//
// dex:field attribute-key:linear-issue-request value-type:json editable:false description:"Issue request"
// dex:field attribute-key:linear-issue-outcome value-type:json editable:false description:"Linear outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := requestInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"linear-issue-request": request, "linear-issue-outcome": outcome}}, nil
}

// GetDexDisplay returns the issue request and the issue, state, and comment Linear returned.
//
// dex:field attribute-key:linear-issue-request value-type:json editable:false description:"Team, title, assignee, state, and comment"
// dex:field attribute-key:linear-issue-outcome value-type:json editable:false description:"Action, issue, state, and comment"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := requestInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"linear-issue-request": request, "linear-issue-outcome": outcome}}, nil
}

// BuildIssueRequest validates Start Flow input, so no connector Step receives an unusable request. A
// picked team wins over the input's teamId.
func BuildIssueRequest(input Input, pickedTeamID string) (IssueRequest, error) {
	request := IssueRequest{
		TeamID: strings.ToLower(strings.TrimSpace(input.TeamID)), Title: strings.TrimSpace(input.Title),
		Description: input.Description, AssigneeEmail: strings.TrimSpace(input.AssigneeEmail),
		StateName: strings.TrimSpace(input.StateName), Comment: strings.TrimSpace(input.Comment),
	}
	if pickedTeamID != "" {
		request.TeamID = pickedTeamID
	}
	if request.Comment == "" {
		request.Comment = DefaultComment
	}
	switch {
	case request.TeamID == "":
		return IssueRequest{}, fmt.Errorf("%w: save the Request team picker or enter the team UUID as teamId", errInvalidIssueRequest)
	case request.Title == "" || strings.ContainsAny(request.Title, "\r\n"):
		return IssueRequest{}, fmt.Errorf("%w: title is required and must be one line", errInvalidIssueRequest)
	case request.AssigneeEmail != "" && !strings.Contains(request.AssigneeEmail, "@"):
		return IssueRequest{}, fmt.Errorf("%w: assigneeEmail must be an email address such as alice@example.com", errInvalidIssueRequest)
	}
	return request, nil
}

// MapToFindUserByEmailInput resolves the requested assignee.
func MapToFindUserByEmailInput(request IssueRequest) linear.FindUserByEmailInput {
	return linear.FindUserByEmailInput{Email: request.AssigneeEmail}
}

// MapToSearchIssuesInput lists the team's open issues with exactly the requested title.
func MapToSearchIssuesInput(request IssueRequest) linear.SearchIssuesInput {
	return linear.SearchIssuesInput{
		Filter:   linear.IssueSearchFilter{TeamID: request.TeamID, Title: request.Title, StateTypes: openStateTypes},
		PageSize: openIssueSearchPageSize,
	}
}

// MapToCreateIssueInput creates the issue, already assigned when the assignee was resolved.
func MapToCreateIssueInput(request IssueRequest) linear.CreateIssueInput {
	return linear.CreateIssueInput{TeamID: request.TeamID, Title: request.Title, Description: request.Description, AssigneeID: request.AssigneeID}
}

// MapToListWorkflowStatesInput lists the request team's workflow states.
func MapToListWorkflowStatesInput(request IssueRequest) linear.ListWorkflowStatesInput {
	return linear.ListWorkflowStatesInput{TeamID: request.TeamID}
}

// MapToUpdateIssueInput sets the chosen state and, when resolved, the assignee.
func MapToUpdateIssueInput(change IssueChange) linear.UpdateIssueInput {
	return linear.UpdateIssueInput{IssueID: change.IssueID, StateID: change.StateID, AssigneeID: change.AssigneeID}
}

// MapToAddCommentInput adds the comment.
func MapToAddCommentInput(comment IssueComment) linear.AddCommentInput {
	return linear.AddCommentInput{IssueID: comment.IssueID, Body: comment.Body}
}

// MapToGetIssueInput reads the issue back.
func MapToGetIssueInput(reference IssueReference) linear.GetIssueInput {
	return linear.GetIssueInput{IssueID: reference.IssueID}
}

// ChooseOpenIssue returns the first listed issue whose title is exactly the request's title and whose
// state is still open, or false when the page has none.
func ChooseOpenIssue(issues []linear.IssueSummary, request IssueRequest) (linear.IssueSummary, bool) {
	for _, issue := range issues {
		if issue.Title == request.Title && strings.EqualFold(issue.Team.ID, request.TeamID) && !issue.State.Type.IsClosed() && issue.ArchivedAt == nil {
			return issue, true
		}
	}
	return linear.IssueSummary{}, false
}

// ChooseTargetState returns the requested state by name, or the team's first unstarted state when the
// request names none.
func ChooseTargetState(states linear.ListWorkflowStatesOutput, stateName string) (linear.WorkflowState, bool) {
	if stateName != "" {
		return states.StateNamed(stateName)
	}
	return states.FirstStateOfType(linear.WorkflowStateTypeUnstarted)
}

func requestInspection(ctx dex.Context) (IssueRequest, IssueRequestOutcome, error) {
	request, err := optionalAttribute(ctx, issueRequestAttribute)
	if err != nil {
		return IssueRequest{}, IssueRequestOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, issueOutcomeAttribute)
	if err != nil {
		return IssueRequest{}, IssueRequestOutcome{}, err
	}
	return request, outcome, nil
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

func failureMessage(failure *sdkgo.Failure) string {
	if failure == nil {
		return ""
	}
	return failure.Message
}

// dex:group group-id:request group-label:"Issue request"
// dex:explanation text:"Validate the issue request with the picked team and record it before calling Linear."
type recordIssueRequest struct {
	dex.StepDefaults
	teamID string
}

func (recordIssueRequest) GetStepType() string { return recordIssueRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordIssueRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordIssueRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildIssueRequest(input, step.teamID)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := issueRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	if err := issueOutcomeAttribute.Set(ctx, IssueRequestOutcome{}); err != nil {
		return nil, err
	}
	if request.AssigneeEmail == "" {
		return dex.GoTo(sdkgo.StepRef[IssueRequest](findOpenIssueStepType), request), nil
	}
	return dex.GoTo(sdkgo.StepRef[IssueRequest](findAssigneeStepType), request), nil
}

// dex:group group-id:request group-label:"Issue request"
// dex:explanation text:"Record the resolved assignee, or that no user has the email, and search for an open issue."
type recordAssignee struct {
	dex.StepDefaultsNoWaitFor[linear.FindUserByEmailResult]
}

func (recordAssignee) GetStepType() string { return recordAssigneeStepType }

func (recordAssignee) Execute(ctx dex.Context, result linear.FindUserByEmailResult) (*dex.StepDecision, error) {
	request, err := issueRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := issueOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if result.Branch == linear.FindUserByEmailBranchFound {
		request.AssigneeID, outcome.AssigneeID = result.Value.ID, result.Value.ID
	} else {
		outcome.IsAssigneeUnknown = true
	}
	if err := issueRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	if err := issueOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[IssueRequest](findOpenIssueStepType), request), nil
}

// dex:group group-id:request group-label:"Issue request"
// dex:explanation text:"Reuse the team's open issue with the exact title, or create the issue when there is none."
type chooseOpenIssue struct {
	dex.StepDefaultsNoWaitFor[linear.SearchIssuesResult]
}

func (chooseOpenIssue) GetStepType() string { return chooseOpenIssueStepType }

func (chooseOpenIssue) Execute(ctx dex.Context, result linear.SearchIssuesResult) (*dex.StepDecision, error) {
	request, err := issueRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	issue, isFound := ChooseOpenIssue(result.Value.Issues, request)
	if !isFound {
		return dex.GoTo(sdkgo.StepRef[IssueRequest](createRequestedIssueStepType), request), nil
	}
	outcome, err := issueOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Action, outcome.IssueID, outcome.Identifier, outcome.URL = IssueReused, issue.ID, issue.Identifier, issue.URL
	if err := issueOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[IssueRequest](listTeamStatesStepType), request), nil
}

// dex:group group-id:request group-label:"Issue request"
// dex:explanation text:"Record the created issue and list the team's workflow states."
type recordCreatedIssue struct {
	dex.StepDefaultsNoWaitFor[linear.CreateIssueResult]
}

func (recordCreatedIssue) GetStepType() string { return recordCreatedIssueStepType }

func (recordCreatedIssue) Execute(ctx dex.Context, result linear.CreateIssueResult) (*dex.StepDecision, error) {
	request, err := issueRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := issueOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Action, outcome.IssueID, outcome.WasCreateReplayed = IssueCreated, result.Value.IssueID, result.Value.IsReplayed
	if issue := result.Value.Issue; issue != nil {
		outcome.Identifier, outcome.URL = issue.Identifier, issue.URL
	}
	if err := issueOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[IssueRequest](listTeamStatesStepType), request), nil
}

// dex:group group-id:request group-label:"Issue request"
// dex:explanation text:"Record Linear's credential-free rejection of the create and fail the Flow; no issue exists."
type recordRejectedIssue struct {
	dex.StepDefaultsNoWaitFor[linear.CreateIssueResult]
}

func (recordRejectedIssue) GetStepType() string { return recordRejectedIssueStepType }

func (recordRejectedIssue) Execute(ctx dex.Context, result linear.CreateIssueResult) (*dex.StepDecision, error) {
	outcome, err := issueOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Action, outcome.RejectionDetail = IssueRejected, failureMessage(result.Failure)
	if err := issueOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.ForceFail("Linear rejected the issue: " + outcome.RejectionDetail), nil
}

// dex:group group-id:request group-label:"Issue request"
// dex:explanation text:"Choose the requested workflow state, or the team's first unstarted state, and fail when the team has none."
type chooseTargetState struct {
	dex.StepDefaultsNoWaitFor[linear.ListWorkflowStatesResult]
}

func (chooseTargetState) GetStepType() string { return chooseTargetStateStepType }

func (chooseTargetState) Execute(ctx dex.Context, result linear.ListWorkflowStatesResult) (*dex.StepDecision, error) {
	request, err := issueRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	state, isFound := ChooseTargetState(result.Value, request.StateName)
	if !isFound {
		names := make([]string, 0, len(result.Value.States))
		for _, available := range result.Value.States {
			names = append(names, available.Name)
		}
		return dex.ForceFail(fmt.Sprintf("the team has no workflow state %q; available states: %s", request.StateName, strings.Join(names, ", "))), nil
	}
	outcome, err := issueOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.StateName = state.Name
	if err := issueOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[IssueChange](moveIssueStepType),
		IssueChange{IssueID: outcome.IssueID, StateID: state.ID, AssigneeID: request.AssigneeID}), nil
}

// dex:group group-id:request group-label:"Issue request"
// dex:explanation text:"Prepare the comment for the moved issue."
type recordMovedIssue struct {
	dex.StepDefaultsNoWaitFor[linear.UpdateIssueResult]
}

func (recordMovedIssue) GetStepType() string { return recordMovedIssueStepType }

func (recordMovedIssue) Execute(ctx dex.Context, result linear.UpdateIssueResult) (*dex.StepDecision, error) {
	request, err := issueRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	issueID := result.Value.IssueID
	if result.Value.Issue != nil {
		issueID = result.Value.Issue.ID
	}
	return dex.GoTo(sdkgo.StepRef[IssueComment](addIssueCommentStepType), IssueComment{IssueID: issueID, Body: request.Comment}), nil
}

// dex:group group-id:request group-label:"Issue request"
// dex:explanation text:"Record the comment and read the issue back."
type recordComment struct {
	dex.StepDefaultsNoWaitFor[linear.AddCommentResult]
}

func (recordComment) GetStepType() string { return recordCommentStepType }

func (recordComment) Execute(ctx dex.Context, result linear.AddCommentResult) (*dex.StepDecision, error) {
	outcome, err := issueOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.CommentID = result.Value.CommentID
	if err := issueOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[IssueReference](readBackIssueStepType), IssueReference{IssueID: outcome.IssueID}), nil
}

// dex:group group-id:request group-label:"Issue request"
// dex:explanation text:"Record the issue's workflow state as read back and complete the Flow."
type completeIssueRequest struct {
	dex.StepDefaultsNoWaitFor[linear.GetIssueResult]
}

func (completeIssueRequest) GetStepType() string { return completeIssueRequestStepType }

func (completeIssueRequest) Execute(ctx dex.Context, result linear.GetIssueResult) (*dex.StepDecision, error) {
	outcome, err := issueOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.ReadBackStateType = result.Value.State.Type
	if err := issueOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
