// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package issuechanged demonstrates the Jira issueChanged Trigger: every issue created or updated in the
// picked project starts one Flow, which reads the issue back and records its current state.
package issuechanged

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each issue change starts.
	FlowType = "JiraIssueChange"
	// ConnectionName is the static Dex Web connection for Jira.
	ConnectionName = "jira-issue-watcher"
	// IssueChangedTriggerBinding is the stable issueChanged binding that starts this Flow.
	IssueChangedTriggerBinding = "project-issue-changes"

	recordIssueChangeStepType   = "RecordIssueChange"
	readChangedIssueStepType    = "ReadChangedIssue"
	completeIssueChangeStepType = "CompleteIssueChange"
)

var (
	issueChangeAttribute        = dex.DefineAttribute[Input]("jira-issue-change")
	issueChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("jira-issue-change-outcome")
)

// Input is one issueChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change jira.IssueChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// IssueKey is the changed issue's key at delivery.
	IssueKey string `json:"issueKey"`
	// ChangeKind is the delivered change kind.
	ChangeKind jira.IssueChangeKind `json:"changeKind"`
	// Current is the issue read back from Jira, with its plain-text description.
	Current jira.Issue `json:"current"`
}

// Flow records one Jira issue change.
type Flow struct {
	dex.FlowDefaults
	connection jira.Connection
}

// NewFlow binds the Jira Connection at registration time.
func NewFlow(connection jira.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Jira connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordIssueChange{}),
		dex.DefineStep(jira.NewGetIssueStep(jira.GetIssueStepConfig[Input]{
			StepType: readChangedIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed issue back with its description, so the record holds its current state.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetIssueInput,
			Found: sdkgo.GoTo(completeIssueChange{}),
		})),
		dex.DefineStep(completeIssueChange{}),
	}
}

// GetConnectorTriggerBindings declares the issueChanged binding and its project picker for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		jira.DefineIssueChangedTriggerBinding(jira.IssueChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: IssueChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "watchedProject", UnitID: jira.UIUnitProjectPicker, Label: "Watched project",
				Description: "Choose the Jira project whose created and updated issues start this Flow; the list comes from this connection's Jira site and shows the projects the account can browse, and the unit saves the project ID, key, and name. Leave it unsaved to watch every project the account can browse. The first start records existing issues as a baseline and starts no Flow for them. Restart the Worker after saving; a changed project records a new baseline.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: jira.UIProjectPickerPortProjectID, JSONPointer: "/projectId"},
					{Port: jira.UIProjectPickerPortProjectKey, JSONPointer: "/projectKey"},
					{Port: jira.UIProjectPickerPortProjectName, JSONPointer: "/projectName"},
				},
			}}},
		}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the change and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{issueChangeAttribute, issueChangeOutcomeAttribute}}
}

// GetDexSummary returns the change kind and outcome.
//
// dex:field attribute-key:jira-issue-change value-type:json editable:false description:"Delivered issue change"
// dex:field attribute-key:jira-issue-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := issueChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"jira-issue-change":         change,
		"jira-issue-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the issue read back from Jira.
//
// dex:field attribute-key:jira-issue-change value-type:json editable:false description:"Site, change kind, and issue from the search page"
// dex:field attribute-key:jira-issue-change-outcome value-type:json editable:false description:"Current issue read back from Jira"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := issueChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"jira-issue-change":         change,
		"jira-issue-change-outcome": outcome,
	}}, nil
}

func issueChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, issueChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, issueChangeOutcomeAttribute)
	return change, outcome, err
}

// optionalAttribute returns the zero value while a running Flow has not set the Attribute yet.
func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// ResolveFlowID names one Flow per issue version; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[jira.IssueChangedEvent]) string {
	return "jira-issue-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[jira.IssueChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryIssueChange admits every change; an application narrows it, for example to new issues.
func AcceptEveryIssueChange(sdkgo.TriggerEvent[jira.IssueChangedEvent]) bool { return true }

// MapToGetIssueInput reads the changed issue by its stable ID, which survives a project move.
func MapToGetIssueInput(input Input) jira.GetIssueInput {
	return jira.GetIssueInput{IssueIDOrKey: input.Change.Issue.ID}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered issue change, then read the issue back."
type recordIssueChange struct {
	dex.StepDefaults
}

func (recordIssueChange) GetStepType() string { return recordIssueChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordIssueChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordIssueChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := issueChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedIssueStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the issue read back from Jira as the outcome and complete the Flow."
type completeIssueChange struct {
	dex.StepDefaultsNoWaitFor[jira.GetIssueResult]
}

func (completeIssueChange) GetStepType() string { return completeIssueChangeStepType }

func (completeIssueChange) Execute(ctx dex.Context, result jira.GetIssueResult) (*dex.StepDecision, error) {
	change, err := issueChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{IssueKey: change.Change.Issue.Key, ChangeKind: change.Change.ChangeKind, Current: result.Value}
	if err := issueChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.CloudID) == "" || strings.TrimSpace(input.Change.Issue.ID) == "":
		return errors.New("change.cloudId and change.issue.id are required")
	}
	switch input.Change.ChangeKind {
	case jira.IssueChangeCreated, jira.IssueChangeUpdated:
		return nil
	default:
		return errors.New("change.changeKind must be created or updated")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
