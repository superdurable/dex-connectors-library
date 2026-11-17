// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/lucid/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// DocumentDetailsFlow reads selected metadata for one Lucid document.
type DocumentDetailsFlow struct {
	dex.FlowDefaults
	connection lucid.Connection
}

// NewDocumentDetailsFlow binds the provider connection at registration time.
func NewDocumentDetailsFlow(connection lucid.Connection) *DocumentDetailsFlow {
	return &DocumentDetailsFlow{connection: connection}
}

func (*DocumentDetailsFlow) GetFlowType() string { return "LucidDocumentDetails" }

var documentDetailsAttribute = dex.DefineAttribute[lucid.DocumentDetails]("lucid-document-details")

func (*DocumentDetailsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{documentDetailsAttribute}}
}

func (flow *DocumentDetailsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDocumentDetails{}),
		dex.DefineStep(lucid.NewGetDocumentStep(lucid.GetDocumentStepConfig[lucid.GetDocumentInput]{
			StepType: "CallGetDocument", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Lucid", Explanation: "Read selected metadata for the requested Lucid document."},
			Connection:  flow.connection, MapToOperationInput: mapDocumentDetailsInput,
			Found: sdkgo.GoTo(completeDocumentDetails{}),
		})),
		dex.DefineStep(completeDocumentDetails{}),
	}
}

func (flow *DocumentDetailsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:lucid-document-details value-type:json editable:false description:"Document metadata"
// GetDexSummary shows selected document metadata in Dex Web.
func (*DocumentDetailsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readDocumentDetails(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lucid-document-details": value}}, nil
}

// dex:field attribute-key:lucid-document-details value-type:json editable:false description:"Document metadata"
// GetDexDisplay shows selected document metadata in Dex Web.
func (*DocumentDetailsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readDocumentDetails(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lucid-document-details": value}}, nil
}

func readDocumentDetails(ctx dex.Context) (lucid.DocumentDetails, error) {
	value, err := documentDetailsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return lucid.DocumentDetails{}, nil
	}
	return value, err
}

func mapDocumentDetailsInput(input lucid.GetDocumentInput) lucid.GetDocumentInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected Lucid document ID to the provider Step."
type startDocumentDetails struct{ dex.StepDefaults }

func (startDocumentDetails) GetStepType() string { return "StartGetDocument" }
func (startDocumentDetails) WaitFor(dex.Context, lucid.GetDocumentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startDocumentDetails) Execute(_ dex.Context, input lucid.GetDocumentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[lucid.GetDocumentInput]("CallGetDocument"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the selected document metadata and complete the Flow."
type completeDocumentDetails struct {
	dex.StepDefaultsNoWaitFor[lucid.GetDocumentResult]
}

func (completeDocumentDetails) GetStepType() string { return "CompleteGetDocument" }
func (completeDocumentDetails) Execute(ctx dex.Context, result lucid.GetDocumentResult) (*dex.StepDecision, error) {
	if err := documentDetailsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*DocumentDetailsFlow)(nil)
