// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contactsync defines a runnable Brevo contact upsert Flow.
package contactsync

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/brevo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "BrevoContactSync"
	// ConnectionName is the static Dex Web connection used by the Flow.
	ConnectionName          = "brevo"
	upsertStepType          = "UpsertBrevoContact"
	listAttributesStepType  = "ListBrevoAttributes"
	updateSenderStepType    = "UpdateBrevoSender"
	updateAttributeStepType = "UpdateBrevoAttribute"
)

var outcomeAttribute = dex.DefineAttribute[brevo.ContactWriteResult]("brevo-contact-sync-outcome")
var senderUpdateAttribute = dex.DefineAttribute[brevo.UpdateSenderInput]("brevo-sender-update")
var attributeUpdateAttribute = dex.DefineAttribute[brevo.UpdateAttributeInput]("brevo-attribute-update")

// Request is the Dex Web Start Flow input.
type Request struct {
	// Email identifies the Brevo contact.
	Email string `json:"email"`
	// FirstName is the contact's first name.
	FirstName string `json:"firstName"`
	// SenderID optionally identifies a sender whose name should be updated.
	SenderID int64 `json:"senderId,omitempty"`
	// SenderName is required when SenderID is supplied.
	SenderName string `json:"senderName,omitempty"`
	// AttributeName optionally names a normal multiple-choice contact attribute.
	AttributeName string `json:"attributeName,omitempty"`
	// AttributeOptions replaces every option when AttributeName is supplied.
	AttributeOptions []string `json:"attributeOptions,omitempty"`
}

// Flow creates or updates one Brevo contact.
type Flow struct {
	dex.FlowDefaults
	connection brevo.Connection
}

// NewFlow binds the configured Brevo connection.
func NewFlow(connection brevo.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute, senderUpdateAttribute, attributeUpdateAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(start{}),
		dex.DefineStep(brevo.NewCreateOrUpdateContactStep(brevo.CreateOrUpdateContactStepConfig[Request]{
			StepType: upsertStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "contact-sync", GroupLabel: "Contact sync", Explanation: "Create or update the contact by email in Brevo."},
			MapToOperationInput: MapToCreateOrUpdateContactInput,
			Saved:               sdkgo.GoTo(saved{}),
		})),
		dex.DefineStep(saved{}),
		dex.DefineStep(brevo.NewListAttributesStep(brevo.ListAttributesStepConfig[brevo.ListAttributesInput]{
			StepType: listAttributesStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "contact-sync", GroupLabel: "Contact sync", Explanation: "Discover contact attribute definitions in Brevo."},
			MapToOperationInput: func(input brevo.ListAttributesInput) brevo.ListAttributesInput { return input },
			Listed:              sdkgo.GoTo(completeAfterAttributes{}),
		})),
		dex.DefineStep(completeAfterAttributes{}),
		dex.DefineStep(brevo.NewUpdateSenderStep(brevo.UpdateSenderStepConfig[brevo.UpdateSenderInput]{
			StepType: updateSenderStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "contact-sync", GroupLabel: "Contact sync", Explanation: "Set the chosen sender's display name in Brevo."},
			MapToOperationInput: func(input brevo.UpdateSenderInput) brevo.UpdateSenderInput { return input },
			Updated:             sdkgo.GoTo(completeAfterSender{}),
		})),
		dex.DefineStep(completeAfterSender{}),
		dex.DefineStep(brevo.NewUpdateAttributeStep(brevo.UpdateAttributeStepConfig[brevo.UpdateAttributeInput]{
			StepType: updateAttributeStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "contact-sync", GroupLabel: "Contact sync", Explanation: "Replace the selected contact attribute's options in Brevo."},
			MapToOperationInput: func(input brevo.UpdateAttributeInput) brevo.UpdateAttributeInput { return input },
			Updated:             sdkgo.GoTo(completeAfterAttribute{}),
		})),
		dex.DefineStep(completeAfterAttribute{}),
	}
}

// MapToCreateOrUpdateContactInput maps a validated request to absolute contact fields.
func MapToCreateOrUpdateContactInput(request Request) brevo.CreateOrUpdateContactInput {
	return brevo.CreateOrUpdateContactInput{Email: request.Email, Attributes: map[string]string{"FNAME": request.FirstName}}
}

// GetRPCs registers the Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetDexSummary returns the completed provider outcome.
// dex:field attribute-key:brevo-contact-sync-outcome value-type:json editable:false description:"Contact save outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"brevo-contact-sync-outcome": outcome}}, nil
}

// GetDexDisplay returns the completed provider outcome.
// dex:field attribute-key:brevo-contact-sync-outcome value-type:json editable:false description:"Contact save outcome"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"brevo-contact-sync-outcome": outcome}}, nil
}

func optionalOutcome(ctx dex.Context) (brevo.ContactWriteResult, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return brevo.ContactWriteResult{}, nil
	}
	return outcome, err
}

// dex:group group-id:contact-sync group-label:"Contact sync"
// dex:explanation text:"Validate and trim the contact request before calling Brevo."
type start struct{ dex.StepDefaults }

func (start) GetStepType() string                             { return "StartBrevoContactSync" }
func (start) WaitFor(dex.Context, Request) (*dex.Wait, error) { return dex.SkipWaitImmediately(), nil }
func (start) Execute(ctx dex.Context, request Request) (*dex.StepDecision, error) {
	request.Email = strings.TrimSpace(request.Email)
	request.FirstName = strings.TrimSpace(request.FirstName)
	request.SenderName = strings.TrimSpace(request.SenderName)
	request.AttributeName = strings.TrimSpace(request.AttributeName)
	if request.Email == "" || request.FirstName == "" || request.SenderID < 0 || (request.SenderID > 0 && request.SenderName == "") || (request.AttributeName != "" && len(request.AttributeOptions) == 0) || (request.AttributeName == "" && len(request.AttributeOptions) > 0) {
		return dex.ForceFail("email and firstName are required"), nil
	}
	if request.SenderID > 0 {
		if err := senderUpdateAttribute.Set(ctx, brevo.UpdateSenderInput{ID: request.SenderID, Name: request.SenderName}); err != nil {
			return nil, err
		}
	}
	if request.AttributeName != "" {
		if err := attributeUpdateAttribute.Set(ctx, brevo.UpdateAttributeInput{Category: "normal", Name: request.AttributeName, Options: request.AttributeOptions}); err != nil {
			return nil, err
		}
	}
	return dex.GoTo(sdkgo.StepRef[Request](upsertStepType), request), nil
}

// dex:group group-id:contact-sync group-label:"Contact sync"
// dex:explanation text:"Complete with Brevo's contact create or update confirmation."
type saved struct {
	dex.StepDefaultsNoWaitFor[brevo.CreateOrUpdateContactResult]
}

func (saved) GetStepType() string { return "BrevoContactSaved" }
func (saved) Execute(ctx dex.Context, result brevo.CreateOrUpdateContactResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[brevo.ListAttributesInput](listAttributesStepType), brevo.ListAttributesInput{}), nil
}

// dex:group group-id:contact-sync group-label:"Contact sync"
// dex:explanation text:"Complete after Brevo lists contact attributes."
type completeAfterAttributes struct {
	dex.StepDefaultsNoWaitFor[brevo.ListAttributesResult]
}

func (completeAfterAttributes) Execute(ctx dex.Context, _ brevo.ListAttributesResult) (*dex.StepDecision, error) {
	sender, err := senderUpdateAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err == nil {
		return dex.GoTo(sdkgo.StepRef[brevo.UpdateSenderInput](updateSenderStepType), sender), nil
	}
	if !errors.As(err, &missing) {
		return nil, err
	}
	attribute, err := attributeUpdateAttribute.Get(ctx)
	if err == nil {
		return dex.GoTo(sdkgo.StepRef[brevo.UpdateAttributeInput](updateAttributeStepType), attribute), nil
	}
	if !errors.As(err, &missing) {
		return nil, err
	}
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:contact-sync group-label:"Contact sync"
// dex:explanation text:"Complete after Brevo updates the selected sender."
type completeAfterSender struct {
	dex.StepDefaultsNoWaitFor[brevo.UpdateSenderResult]
}

func (completeAfterSender) Execute(ctx dex.Context, _ brevo.UpdateSenderResult) (*dex.StepDecision, error) {
	attribute, err := attributeUpdateAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err == nil {
		return dex.GoTo(sdkgo.StepRef[brevo.UpdateAttributeInput](updateAttributeStepType), attribute), nil
	}
	if !errors.As(err, &missing) {
		return nil, err
	}
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:contact-sync group-label:"Contact sync"
// dex:explanation text:"Complete after Brevo updates the selected contact attribute."
type completeAfterAttribute struct {
	dex.StepDefaultsNoWaitFor[brevo.UpdateAttributeResult]
}

func (completeAfterAttribute) Execute(ctx dex.Context, _ brevo.UpdateAttributeResult) (*dex.StepDecision, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
