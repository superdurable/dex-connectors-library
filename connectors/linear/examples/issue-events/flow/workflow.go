// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package issueevents starts one Flow per newly created Linear issue from the issueEventReceived Trigger,
// reads the issue back with getIssue, and records both.
package issueevents

import (
	"errors"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity shown in Dex Web.
	FlowType = "LinearIssueEventRecorder"
	// ConnectionName is the static Dex Web connection that verifies webhooks and reads issues.
	ConnectionName = "linear-workspace"
	// IssueCreatedTriggerBinding is the issueEventReceived binding whose create events start this Flow.
	IssueCreatedTriggerBinding = "issue-created"
	// FlowIDPrefix precedes the Trigger event ID in every Flow ID, so a redelivered webhook maps to one Flow.
	FlowIDPrefix = "linear-issue-"

	readEventIssueStepType = "ReadEventIssue"
)

var (
	issueEventAttribute = dex.DefineAttribute[CreatedIssueEvent]("linear-issue-event")
	issueReadAttribute  = dex.DefineAttribute[RecordedIssue]("linear-issue-read")
)

// CreatedIssueEvent is the Flow's start input: one verified Issue create webhook.
type CreatedIssueEvent struct {
	// EventID is the Trigger event ID, such as create:<issue UUID>:<updatedAt milliseconds>.
	EventID string `json:"eventId"`
	// IssueID is the created issue's UUID.
	IssueID string `json:"issueId"`
	// Identifier is the issue's identifier, such as ENG-123.
	Identifier string `json:"identifier"`
	// Title is the issue's title when it was created.
	Title string `json:"title"`
	// TeamID is the issue's team UUID.
	TeamID string `json:"teamId"`
	// CreatedAt is when Linear created the event payload.
	CreatedAt time.Time `json:"createdAt"`
}

// RecordedIssue is the getIssue outcome the Flow stores; it completes the Flow when found.
type RecordedIssue struct {
	// Branch is the getIssue branch: found, notFound, providerRejected, invalidResponse, or defect.
	Branch sdkgo.BranchID `json:"branch"`
	// Issue is the issue Linear returned on the found branch.
	Issue *linear.Issue `json:"issue,omitempty"`
	// FailureKind is the safe failure category of a branch other than found.
	FailureKind sdkgo.FailureKind `json:"failureKind,omitempty"`
	// FailureMessage is the safe failure message, which never holds keys or Linear response text.
	FailureMessage string `json:"failureMessage,omitempty"`
}

// Flow records one created issue and reads it back.
type Flow struct {
	dex.FlowDefaults
	connection linear.Connection
}

// NewFlow binds the Linear Connection that reads issues.
func NewFlow(connection linear.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the record, read, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordIssueEvent{}),
		dex.DefineStep(linear.NewGetIssueStep(linear.GetIssueStepConfig[CreatedIssueEvent]{
			StepType: readEventIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "linear", GroupLabel: "Linear",
				Explanation: "Read the created issue back from Linear by its UUID.",
			},
			Connection:          flow.connection,
			MapToOperationInput: MapToGetIssueInput,
			Found:               sdkgo.GoTo(recordIssueRead{}),
			NotFound:            sdkgo.GoTo(recordReadFailure{}),
			ProviderRejected:    sdkgo.GoTo(recordReadFailure{}),
			InvalidResponse:     sdkgo.GoTo(recordReadFailure{}),
			Defect:              sdkgo.GoTo(recordReadFailure{}),
		})),
		dex.DefineStep(recordIssueRead{}),
		dex.DefineStep(recordReadFailure{}),
	}
}

// GetRPCs returns the summary and display RPCs that Dex Web shows.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the event and issue Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{issueEventAttribute, issueReadAttribute}}
}

// GetConnectorTriggerBindings declares the issueEventReceived binding that starts this Flow.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		linear.DefineIssueEventReceivedTriggerBinding(linear.IssueEventReceivedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: IssueCreatedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "team", UnitID: linear.UIUnitTeamPicker, Label: "Team",
				Description: "Choose the Linear team whose new issues start this Flow; an OAuth connection lists its teams, and with a personal API key paste the team UUID from Linear Settings > Teams > the team > Copy team ID. Leave it empty to record new issues of every team the webhook sends.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: linear.UITeamPickerPortTeamID, JSONPointer: "/teamId"}},
			}}},
		}),
	}
}

// GetDexSummary returns the event and issue for the Dex Web run list.
//
// dex:field attribute-key:linear-issue-event value-type:json editable:false description:"Created issue event"
// dex:field attribute-key:linear-issue-read value-type:json editable:false description:"Issue read back from Linear"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	event, recorded, err := eventInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"linear-issue-event": event, "linear-issue-read": recorded}}, nil
}

// GetDexDisplay returns the event and issue for the Dex Web run detail.
//
// dex:field attribute-key:linear-issue-event value-type:json editable:false description:"Issue UUID, identifier, title, and team"
// dex:field attribute-key:linear-issue-read value-type:json editable:false description:"getIssue branch and issue"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	event, recorded, err := eventInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"linear-issue-event": event, "linear-issue-read": recorded}}, nil
}

// AcceptIssueCreated is the application's admission rule: only a create event of an issue that is not
// in the trash starts a Flow.
func AcceptIssueCreated(event sdkgo.TriggerEvent[linear.IssueEvent]) bool {
	return event.Payload.Action == linear.IssueEventActionCreate && !event.Payload.Issue.IsTrashed
}

// ResolveFlowID derives the Flow ID from the event ID, so every redelivery maps to one Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[linear.IssueEvent]) string {
	return FlowIDPrefix + strings.ReplaceAll(event.ID, ":", "-")
}

// MapToFlowInput copies the verified event into the Flow's start input.
func MapToFlowInput(event sdkgo.TriggerEvent[linear.IssueEvent]) CreatedIssueEvent {
	issue := event.Payload.Issue
	return CreatedIssueEvent{
		EventID: event.ID, IssueID: issue.ID, Identifier: issue.Identifier, Title: issue.Title, TeamID: issue.Team.ID, CreatedAt: event.OccurredAt,
	}
}

// MapToGetIssueInput reads the created issue by its UUID, which survives a move to another team.
func MapToGetIssueInput(event CreatedIssueEvent) linear.GetIssueInput {
	return linear.GetIssueInput{IssueID: event.IssueID}
}

func eventInspection(ctx dex.Context) (CreatedIssueEvent, RecordedIssue, error) {
	event, err := optionalAttribute(ctx, issueEventAttribute)
	if err != nil {
		return CreatedIssueEvent{}, RecordedIssue{}, err
	}
	recorded, err := optionalAttribute(ctx, issueReadAttribute)
	if err != nil {
		return CreatedIssueEvent{}, RecordedIssue{}, err
	}
	return event, recorded, nil
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

// dex:group group-id:event group-label:"Issue event"
// dex:explanation text:"Persist the verified issue event before reading the issue."
type recordIssueEvent struct {
	dex.StepDefaultsNoWaitFor[CreatedIssueEvent]
}

func (recordIssueEvent) GetStepType() string { return "RecordIssueEvent" }

func (recordIssueEvent) Execute(ctx dex.Context, event CreatedIssueEvent) (*dex.StepDecision, error) {
	if event.EventID == "" || event.IssueID == "" {
		return dex.ForceFail("an issue event requires its event ID and issue UUID"), nil
	}
	if err := issueEventAttribute.Set(ctx, event); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CreatedIssueEvent](readEventIssueStepType), event), nil
}

// dex:group group-id:linear group-label:"Linear"
// dex:explanation text:"Persist the issue Linear returned and complete the Flow."
type recordIssueRead struct {
	dex.StepDefaultsNoWaitFor[linear.GetIssueResult]
}

func (recordIssueRead) GetStepType() string { return "RecordIssueRead" }

func (recordIssueRead) Execute(ctx dex.Context, result linear.GetIssueResult) (*dex.StepDecision, error) {
	issue := result.Value
	recorded := RecordedIssue{Branch: result.Branch, Issue: &issue}
	if err := issueReadAttribute.Set(ctx, recorded); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(recorded), nil
}

// dex:group group-id:linear group-label:"Linear"
// dex:explanation text:"Persist the notFound, providerRejected, invalidResponse, or defect outcome with its safe message and fail the Flow."
type recordReadFailure struct {
	dex.StepDefaultsNoWaitFor[linear.GetIssueResult]
}

func (recordReadFailure) GetStepType() string { return "RecordReadFailure" }

func (recordReadFailure) Execute(ctx dex.Context, result linear.GetIssueResult) (*dex.StepDecision, error) {
	recorded := RecordedIssue{Branch: result.Branch}
	message := "reading the issue selected " + string(result.Branch)
	if result.Failure != nil {
		recorded.FailureKind, recorded.FailureMessage = result.Failure.Kind, result.Failure.Message
		message += ": " + result.Failure.Message
	}
	if err := issueReadAttribute.Set(ctx, recorded); err != nil {
		return nil, err
	}
	return dex.ForceFail(message), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
