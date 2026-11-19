// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package agreementinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adobe/acrobat-sign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createAgreementReminderStepType = "CallCreateAgreementReminder"

var createdAgreementReminderAttribute = dex.DefineAttribute[acrobatsign.CreatedAgreementReminder]("adobe-acrobat-sign-created-reminder")

// CreateAgreementReminderFlow creates a one-time reminder and stores its ID.
type CreateAgreementReminderFlow struct {
	dex.FlowDefaults
	connection acrobatsign.Connection
}

// NewCreateAgreementReminderFlow binds the same static Acrobat Sign connection.
func NewCreateAgreementReminderFlow(connection acrobatsign.Connection) *CreateAgreementReminderFlow {
	return &CreateAgreementReminderFlow{connection: connection}
}

// GetFlowType is the stable cancellation example identity.
func (*CreateAgreementReminderFlow) GetFlowType() string {
	return "AdobeAcrobatSignAgreementReminderCreate"
}

// GetPersistenceSchema registers the cancellation result shown by Dex Web.
func (*CreateAgreementReminderFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdAgreementReminderAttribute}}
}

// GetSteps registers one reminder Mutation and a typed completion.
func (flow *CreateAgreementReminderFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAgreementReminderCreate{}),
		dex.DefineStep(acrobatsign.NewCreateAgreementReminderStep(acrobatsign.CreateAgreementReminderStepConfig[acrobatsign.CreateAgreementReminderInput]{
			StepType: createAgreementReminderStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Adobe Acrobat Sign", Explanation: "Create one reminder for selected agreement participants."},
			Connection:  flow.connection, MapToOperationInput: MapToCreateAgreementReminderInput,
			Created: sdkgo.GoTo(completeAgreementReminderCreate{}),
		})),
		dex.DefineStep(completeAgreementReminderCreate{}),
	}
}

// GetRPCs exposes the stored status in Dex Web.
func (flow *CreateAgreementReminderFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:adobe-acrobat-sign-created-reminder value-type:json editable:false description:"Created reminder ID"
// GetDexSummary shows the created reminder ID in Dex Web.
func (*CreateAgreementReminderFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCreatedAgreementReminder(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adobe-acrobat-sign-created-reminder": page}}, nil
}

// dex:field attribute-key:adobe-acrobat-sign-created-reminder value-type:json editable:false description:"Created reminder ID"
// GetDexDisplay shows the created reminder ID in Dex Web.
func (*CreateAgreementReminderFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCreatedAgreementReminder(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adobe-acrobat-sign-created-reminder": page}}, nil
}

func readCreatedAgreementReminder(ctx dex.Context) (acrobatsign.CreatedAgreementReminder, error) {
	page, err := createdAgreementReminderAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return acrobatsign.CreatedAgreementReminder{}, nil
	}
	return page, err
}

// MapToCreateAgreementReminderInput preserves the agreement and participant IDs.
func MapToCreateAgreementReminderInput(input acrobatsign.CreateAgreementReminderInput) acrobatsign.CreateAgreementReminderInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the agreement and participant IDs to the provider Step."
type startAgreementReminderCreate struct{ dex.StepDefaults }

func (startAgreementReminderCreate) GetStepType() string { return "StartCreateAgreementReminder" }
func (startAgreementReminderCreate) WaitFor(dex.Context, acrobatsign.CreateAgreementReminderInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAgreementReminderCreate) Execute(_ dex.Context, input acrobatsign.CreateAgreementReminderInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[acrobatsign.CreateAgreementReminderInput](createAgreementReminderStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the created reminder ID."
type completeAgreementReminderCreate struct {
	dex.StepDefaultsNoWaitFor[acrobatsign.CreateAgreementReminderResult]
}

func (completeAgreementReminderCreate) GetStepType() string { return "CompleteCreateAgreementReminder" }
func (completeAgreementReminderCreate) Execute(ctx dex.Context, result acrobatsign.CreateAgreementReminderResult) (*dex.StepDecision, error) {
	if err := createdAgreementReminderAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateAgreementReminderFlow)(nil)
