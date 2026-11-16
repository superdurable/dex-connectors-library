// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package profilechanged demonstrates the LinkedIn profileChanged Trigger: every change of the connected
// member's own profile claims starts one Flow, which records the change and reads the member's verified
// profile back with the getAuthenticatedProfile Query.
package profilechanged

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/linkedin/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each profile change starts.
	FlowType = "LinkedInProfileChange"
	// ConnectionName is the static Dex Web connection for LinkedIn authorization.
	ConnectionName = "linkedin-profile-watcher"
	// ProfileChangedTriggerBinding is the stable profileChanged binding that starts this Flow.
	ProfileChangedTriggerBinding = "own-profile-changes"

	recordProfileChangeStepType   = "RecordLinkedInProfileChange"
	readVerifiedProfileStepType   = "ReadLinkedInVerifiedProfile"
	completeProfileChangeStepType = "CompleteLinkedInProfileChange"
)

var (
	profileChangeAttribute        = dex.DefineAttribute[Input]("linkedin-profile-change")
	profileChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("linkedin-profile-change-outcome")
)

// Input is one profileChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change linkedin.ProfileChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// Subject is the member whose profile changed.
	Subject string `json:"subject"`
	// ChangedFields repeats the delivered changed claims.
	ChangedFields []linkedin.ProfileField `json:"changedFields"`
	// VerifiedProfile is the profile read back from LinkedIn; nil when the member has no verified email.
	VerifiedProfile *linkedin.AuthenticatedProfile `json:"verifiedProfile,omitempty"`
}

// Flow records one change of the connected LinkedIn member's own profile.
type Flow struct {
	dex.FlowDefaults
	connection linkedin.Connection
}

// NewFlow binds the LinkedIn Connection at registration time.
func NewFlow(connection linkedin.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and LinkedIn connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordProfileChange{}),
		dex.DefineStep(linkedin.NewGetAuthenticatedProfileStep(linkedin.GetAuthenticatedProfileStepConfig[Input]{
			StepType: readVerifiedProfileStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the member's verified profile back from LinkedIn, so the record holds the current verified email.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetAuthenticatedProfileInput,
			ProfileLoaded: sdkgo.GoTo(completeProfileChange{}),
			// Business reaction: a change that unverified or removed the email is recorded without a verified profile.
			VerifiedEmailRequired: sdkgo.GoTo(completeProfileChange{}),
		})),
		dex.DefineStep(completeProfileChange{}),
	}
}

// GetConnectorTriggerBindings declares the profileChanged binding. It watches only the connected member's
// own profile, so Dex Web shows no field for it.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		linkedin.DefineProfileChangedTriggerBinding(linkedin.ProfileChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: ProfileChangedTriggerBinding,
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{profileChangeAttribute, profileChangeOutcomeAttribute}}
}

// GetDexSummary returns the change and outcome.
//
// dex:field attribute-key:linkedin-profile-change value-type:json editable:false description:"Delivered profile change"
// dex:field attribute-key:linkedin-profile-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := profileChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"linkedin-profile-change":         change,
		"linkedin-profile-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the verified profile read back from LinkedIn.
//
// dex:field attribute-key:linkedin-profile-change value-type:json editable:false description:"Previous and current profile with the changed claims"
// dex:field attribute-key:linkedin-profile-change-outcome value-type:json editable:false description:"Verified profile read back from LinkedIn"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := profileChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"linkedin-profile-change":         change,
		"linkedin-profile-change-outcome": outcome,
	}}, nil
}

func profileChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, profileChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, profileChangeOutcomeAttribute)
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

// ResolveFlowID names one Flow per change; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[linkedin.ProfileChangedEvent]) string {
	return "linkedin-profile-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[linkedin.ProfileChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryProfileChange admits every change; an application narrows it, for example to email changes.
func AcceptEveryProfileChange(sdkgo.TriggerEvent[linkedin.ProfileChangedEvent]) bool { return true }

// MapToGetAuthenticatedProfileInput returns the provider Query input, which has no field.
func MapToGetAuthenticatedProfileInput(Input) linkedin.GetAuthenticatedProfileInput {
	return linkedin.GetAuthenticatedProfileInput{}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Validate and record the delivered profile change before reading the verified profile."
type recordProfileChange struct {
	dex.StepDefaults
}

func (recordProfileChange) GetStepType() string { return recordProfileChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordProfileChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordProfileChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := profileChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readVerifiedProfileStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the verified profile, or none without a verified email, and complete the Flow."
type completeProfileChange struct {
	dex.StepDefaultsNoWaitFor[linkedin.GetAuthenticatedProfileResult]
}

func (completeProfileChange) GetStepType() string { return completeProfileChangeStepType }

func (completeProfileChange) Execute(ctx dex.Context, result linkedin.GetAuthenticatedProfileResult) (*dex.StepDecision, error) {
	change, err := profileChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{Subject: change.Change.Subject, ChangedFields: change.Change.ChangedFields}
	if result.Branch == linkedin.GetAuthenticatedProfileBranchProfileLoaded {
		verified := result.Value
		outcome.VerifiedProfile = &verified
	}
	if err := profileChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.Subject) == "":
		return errors.New("change.subject is required")
	case len(input.Change.ChangedFields) == 0:
		return errors.New("change.changedFields is required")
	}
	return nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
