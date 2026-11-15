// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package memberchangeintake demonstrates the Mailchimp memberChanged Trigger: every contact added to or
// changed in the watched audience starts one Flow, which reads the contact back and records its current
// state for a downstream system such as a CRM.
package memberchangeintake

import (
	"errors"
	"strings"

	mailchimp "github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each contact change starts.
	FlowType = "MailchimpMemberChange"
	// ConnectionName is the static Dex Web connection for Mailchimp.
	ConnectionName = "mailchimp-audience"
	// MemberChangedTriggerBinding is the stable memberChanged binding that starts this Flow.
	MemberChangedTriggerBinding = "audience-member-changes"

	recordMemberChangeStepType   = "RecordMemberChange"
	readChangedMemberStepType    = "ReadChangedMember"
	completeMemberChangeStepType = "CompleteMemberChange"
)

var (
	memberChangeAttribute        = dex.DefineAttribute[Input]("mailchimp-member-change")
	memberChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("mailchimp-member-change-outcome")
)

// Input is one memberChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change mailchimp.MemberChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// AudienceID is the watched audience.
	AudienceID string `json:"audienceId"`
	// EmailAddress is the changed contact's address as delivered.
	EmailAddress string `json:"emailAddress"`
	// DeliveredStatus is the contact's status in the delivered change.
	DeliveredStatus mailchimp.MemberStatus `json:"deliveredStatus"`
	// Current is the contact read back from Mailchimp, which may be newer than the delivered change.
	Current mailchimp.Member `json:"current"`
}

// Flow records one Mailchimp contact change.
type Flow struct {
	dex.FlowDefaults
	connection mailchimp.Connection
}

// NewFlow binds the Mailchimp Connection at registration time.
func NewFlow(connection mailchimp.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Mailchimp connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordMemberChange{}),
		dex.DefineStep(mailchimp.NewGetMemberStep(mailchimp.GetMemberStepConfig[Input]{
			StepType: readChangedMemberStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed contact back, so the record holds its current state rather than the polled copy.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetMemberInput,
			Found: sdkgo.GoTo(completeMemberChange{}),
		})),
		dex.DefineStep(completeMemberChange{}),
	}
}

// GetConnectorTriggerBindings declares the memberChanged binding with its audience and status units.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		mailchimp.DefineMemberChangedTriggerBinding(mailchimp.MemberChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: MemberChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "watchedAudience", UnitID: mailchimp.UIUnitAudienceIDEntry, Label: "Watched audience", Required: true,
					Description: "Enter the ID of the audience whose added and changed contacts start this Flow, such as 57afe96172, from Audience > More options > Audience settings in Mailchimp. The first start records existing contacts as a baseline and starts no Flow for them. Restart the Worker after saving; a changed audience records a new baseline.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: mailchimp.UIAudienceIDEntryPortAudienceID, JSONPointer: "/audienceId"}},
				},
				{
					ID: "deliveredStatuses", UnitID: mailchimp.UIUnitMemberStatusPicker, Label: "Contact statuses",
					Description: "Select the statuses a contact must have after its change to start this Flow, for example subscribed for new sign-ups or unsubscribed and cleaned to suppress addresses elsewhere. Select none to start a Flow for every changed contact.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: mailchimp.UIMemberStatusPickerPortStatuses, JSONPointer: "/statuses"}},
				},
			}},
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{memberChangeAttribute, memberChangeOutcomeAttribute}}
}

// GetDexSummary returns the delivered change and its outcome.
//
// dex:field attribute-key:mailchimp-member-change value-type:json editable:false description:"Delivered contact change"
// dex:field attribute-key:mailchimp-member-change-outcome value-type:json editable:false description:"Recorded contact"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := memberChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"mailchimp-member-change":         change,
		"mailchimp-member-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the contact read back from Mailchimp.
//
// dex:field attribute-key:mailchimp-member-change value-type:json editable:false description:"Audience and contact from the poll"
// dex:field attribute-key:mailchimp-member-change-outcome value-type:json editable:false description:"Current contact read back from Mailchimp"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := memberChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"mailchimp-member-change":         change,
		"mailchimp-member-change-outcome": outcome,
	}}, nil
}

func memberChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, memberChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, memberChangeOutcomeAttribute)
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

// ResolveFlowID names one Flow per contact version; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[mailchimp.MemberChangedEvent]) string {
	return "mailchimp-member-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[mailchimp.MemberChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryMemberChange admits every delivered change; the binding's status picker already filters them.
func AcceptEveryMemberChange(sdkgo.TriggerEvent[mailchimp.MemberChangedEvent]) bool { return true }

// MapToGetMemberInput reads the changed contact from its audience.
func MapToGetMemberInput(input Input) mailchimp.GetMemberInput {
	return mailchimp.GetMemberInput{ListID: input.Change.AudienceID, EmailAddress: input.Change.Member.EmailAddress}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Validate and record the delivered contact change, then read the contact back."
type recordMemberChange struct {
	dex.StepDefaults
}

func (recordMemberChange) GetStepType() string { return recordMemberChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordMemberChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordMemberChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := memberChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedMemberStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the contact read back from Mailchimp as the outcome and complete the Flow."
type completeMemberChange struct {
	dex.StepDefaultsNoWaitFor[mailchimp.GetMemberResult]
}

func (completeMemberChange) GetStepType() string { return completeMemberChangeStepType }

func (completeMemberChange) Execute(ctx dex.Context, result mailchimp.GetMemberResult) (*dex.StepDecision, error) {
	change, err := memberChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{
		AudienceID: change.Change.AudienceID, EmailAddress: change.Change.Member.EmailAddress,
		DeliveredStatus: change.Change.Member.Status, Current: result.Value,
	}
	if err := memberChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.AudienceID) == "" || strings.TrimSpace(input.Change.Member.EmailAddress) == "":
		return errors.New("change.audienceId and change.member.emailAddress are required")
	}
	return nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
