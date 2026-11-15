// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package pagechanged demonstrates the Confluence pageChanged Trigger: every page created or updated in
// the picked space starts one Flow, which reads the page back as Markdown and records it.
package pagechanged

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each page change starts.
	FlowType = "ConfluencePageChange"
	// ConnectionName is the static Dex Web connection for Confluence.
	ConnectionName = "confluence-page-watcher"
	// PageChangedTriggerBinding is the stable pageChanged binding that starts this Flow.
	PageChangedTriggerBinding = "space-page-changes"
	// MaxRecordedBodyCharacters bounds the Markdown kept in the outcome.
	MaxRecordedBodyCharacters = 2000

	recordPageChangeStepType   = "RecordPageChange"
	readChangedPageStepType    = "ReadChangedPage"
	completePageChangeStepType = "CompletePageChange"
)

var (
	pageChangeAttribute        = dex.DefineAttribute[Input]("confluence-page-change")
	pageChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("confluence-page-change-outcome")
)

// Input is one pageChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change confluence.PageChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// PageID is the changed page.
	PageID string `json:"pageId"`
	// ChangeKind is the delivered change kind.
	ChangeKind confluence.PageChangeKind `json:"changeKind"`
	// DeliveredVersion is the version the Trigger delivered.
	DeliveredVersion int `json:"deliveredVersion"`
	// CurrentVersion is the version read back; it is later than DeliveredVersion when the page changed again.
	CurrentVersion int `json:"currentVersion"`
	// Title is the page title read back.
	Title string `json:"title"`
	// Body is the start of the page body as Markdown, at most MaxRecordedBodyCharacters characters.
	Body string `json:"body"`
	// WebURL is the page's address in the Confluence web UI.
	WebURL string `json:"webUrl,omitempty"`
}

// Flow records one Confluence page change.
type Flow struct {
	dex.FlowDefaults
	connection confluence.Connection
}

// NewFlow binds the Confluence Connection at registration time.
func NewFlow(connection confluence.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Confluence connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordPageChange{}),
		dex.DefineStep(confluence.NewGetPageStep(confluence.GetPageStepConfig[Input]{
			StepType: readChangedPageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed page back as Markdown, so the record holds its current title and body.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetPageInput,
			Found: sdkgo.GoTo(completePageChange{}),
		})),
		dex.DefineStep(completePageChange{}),
	}
}

// GetConnectorTriggerBindings declares the pageChanged binding and its space picker for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		confluence.DefinePageChangedTriggerBinding(confluence.PageChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: PageChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "watchedSpace", UnitID: confluence.UIUnitSpacePicker, Label: "Watched space",
				Description: "Choose the Confluence space whose created and updated pages start this Flow; the list comes from the current spaces this connection's site lets the account view, and the picker saves the space ID, key, and name, which are not secret and are stored in the project configuration. The same spaces are listed at https://<your-site>.atlassian.net/wiki/spaces, and a key looks like OPS. Leave it unsaved to watch every space the account can view. The first start records recently changed pages as a baseline and starts no Flow for them. Restart the Worker after saving; a changed space records a new baseline.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: confluence.UISpacePickerPortSpaceID, JSONPointer: "/spaceId"},
					{Port: confluence.UISpacePickerPortSpaceKey, JSONPointer: "/spaceKey"},
					{Port: confluence.UISpacePickerPortSpaceName, JSONPointer: "/spaceName"},
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{pageChangeAttribute, pageChangeOutcomeAttribute}}
}

// GetDexSummary returns the delivered change and the recorded outcome.
//
// dex:field attribute-key:confluence-page-change value-type:json editable:false description:"Delivered page change"
// dex:field attribute-key:confluence-page-change-outcome value-type:json editable:false description:"Recorded page"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := pageChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"confluence-page-change":         change,
		"confluence-page-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the page read back from Confluence.
//
// dex:field attribute-key:confluence-page-change value-type:json editable:false description:"Site, change kind, and page version from the search"
// dex:field attribute-key:confluence-page-change-outcome value-type:json editable:false description:"Current title, version, and Markdown read back from Confluence"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := pageChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"confluence-page-change":         change,
		"confluence-page-change-outcome": outcome,
	}}, nil
}

func pageChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, pageChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, pageChangeOutcomeAttribute)
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

// ResolveFlowID names one Flow per page version; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[confluence.PageChangedEvent]) string {
	return "confluence-page-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[confluence.PageChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryPageChange admits every change; an application narrows it, for example to new pages.
func AcceptEveryPageChange(sdkgo.TriggerEvent[confluence.PageChangedEvent]) bool { return true }

// MapToGetPageInput reads the changed page as Markdown, bounded for the record.
func MapToGetPageInput(input Input) confluence.GetPageInput {
	return confluence.GetPageInput{PageID: input.Change.Page.ID, MaxBodyCharacters: MaxRecordedBodyCharacters}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered page change, then read the page back."
type recordPageChange struct {
	dex.StepDefaults
}

func (recordPageChange) GetStepType() string { return recordPageChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordPageChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordPageChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := pageChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedPageStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the page read back from Confluence as the outcome and complete the Flow."
type completePageChange struct {
	dex.StepDefaultsNoWaitFor[confluence.GetPageResult]
}

func (completePageChange) GetStepType() string { return completePageChangeStepType }

func (completePageChange) Execute(ctx dex.Context, result confluence.GetPageResult) (*dex.StepDecision, error) {
	change, err := pageChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	current := result.Value
	outcome := ChangeOutcome{
		PageID: change.Change.Page.ID, ChangeKind: change.Change.ChangeKind, DeliveredVersion: change.Change.Page.VersionNumber,
		CurrentVersion: current.Version.Number, Title: current.Title, Body: current.Body, WebURL: current.WebURL,
	}
	if err := pageChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.Page.ID) == "" || input.Change.Page.VersionNumber < 1:
		return errors.New("change.page.id and change.page.versionNumber are required")
	}
	switch input.Change.ChangeKind {
	case confluence.PageChangeCreated, confluence.PageChangeUpdated:
		return nil
	default:
		return errors.New("change.changeKind must be created or updated")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
