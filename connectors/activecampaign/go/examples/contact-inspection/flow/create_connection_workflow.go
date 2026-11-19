// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createConnectionStepType = "CallCreateConnection"

var createdConnectionAttribute = dex.DefineAttribute[activecampaign.ConnectionMetadata]("activecampaign-created-connection")

// CreateConnectionFlow demonstrates a repeat-safe account create through a Dex Step.
type CreateConnectionFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewCreateConnectionFlow binds the same static connection as the inspection Flow.
func NewCreateConnectionFlow(connection activecampaign.Connection) *CreateConnectionFlow {
	return &CreateConnectionFlow{connection: connection}
}

// GetFlowType is the stable create example identity.
func (*CreateConnectionFlow) GetFlowType() string { return "ActiveCampaignCreateConnection" }

// GetPersistenceSchema registers the created tag displayed in Dex Web.
func (*CreateConnectionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdConnectionAttribute}}
}

// GetSteps registers the start, provider call, and completion boundary.
func (flow *CreateConnectionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateConnection{}),
		dex.DefineStep(activecampaign.NewCreateConnectionStep(activecampaign.CreateConnectionStepConfig[activecampaign.CreateConnectionInput]{
			StepType: createConnectionStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Create one ecommerce connection by name."},
			Connection:  flow.connection, MapToOperationInput: MapToCreateConnectionInput,
			Created: sdkgo.GoTo(completeCreateConnection{}),
		})),
		dex.DefineStep(completeCreateConnection{}),
	}
}

// GetRPCs exposes the stored connection in Dex Web.
func (flow *CreateConnectionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-created-connection value-type:json editable:false description:"Created connection"
// GetDexSummary shows the created connection in Dex Web.
func (*CreateConnectionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	connection, err := readCreatedConnection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-connection": connection}}, nil
}

// dex:field attribute-key:activecampaign-created-connection value-type:json editable:false description:"Created connection"
// GetDexDisplay shows the created connection in Dex Web.
func (*CreateConnectionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	connection, err := readCreatedConnection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-connection": connection}}, nil
}

func readCreatedConnection(ctx dex.Context) (activecampaign.ConnectionMetadata, error) {
	connection, err := createdConnectionAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.ConnectionMetadata{}, nil
	}
	return connection, err
}

// MapToCreateConnectionInput preserves the typed start input for the provider call.
func MapToCreateConnectionInput(input activecampaign.CreateConnectionInput) activecampaign.CreateConnectionInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the connection fields to the create Step."
type startCreateConnection struct{ dex.StepDefaults }

func (startCreateConnection) GetStepType() string { return "StartCreateConnection" }
func (startCreateConnection) WaitFor(dex.Context, activecampaign.CreateConnectionInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateConnection) Execute(_ dex.Context, input activecampaign.CreateConnectionInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.CreateConnectionInput](createConnectionStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created ecommerce connection metadata."
type completeCreateConnection struct {
	dex.StepDefaultsNoWaitFor[activecampaign.CreateConnectionResult]
}

func (completeCreateConnection) GetStepType() string { return "CompleteCreateConnection" }
func (completeCreateConnection) Execute(ctx dex.Context, result activecampaign.CreateConnectionResult) (*dex.StepDecision, error) {
	if err := createdConnectionAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateConnectionFlow)(nil)
