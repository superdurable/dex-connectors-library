// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/constantcontact/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const segmentsStepType = "ListConstantContactSegments"

// SegmentsFlow reads a segment page and completes with the typed result.
type SegmentsFlow struct {
	dex.FlowDefaults
	connection constantcontact.Connection
}

// NewSegmentsFlow binds the provider connection for segment inspection.
func NewSegmentsFlow(connection constantcontact.Connection) *SegmentsFlow {
	return &SegmentsFlow{connection: connection}
}

// GetPersistenceSchema declares the list result retained for Dex Web.
func (*SegmentsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{segmentsAttribute}}
}

// GetSteps wires the operation-specific list Query to a completion Step.
func (flow *SegmentsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSegments{}),
		dex.DefineStep(constantcontact.NewListSegmentsStep(constantcontact.ListSegmentsStepConfig[constantcontact.ListSegmentsInput]{
			StepType: segmentsStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Constant Contact", Explanation: "Read a page of segments from Constant Contact."},
			Connection:          flow.connection,
			MapToOperationInput: mapSegmentsInput,
			Listed:              sdkgo.GoTo(completeSegments{}),
		})),
		dex.DefineStep(completeSegments{}),
	}
}

// GetRPCs exposes read-only Dex Web views of the last list page.
func (flow *SegmentsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:constantcontact-segments value-type:json editable:false description:"Provider list page"
// GetDexSummary shows the last list page in Dex Web.
func (*SegmentsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSegments(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-segments": page}}, nil
}

// dex:field attribute-key:constantcontact-segments value-type:json editable:false description:"Provider segment page"
// GetDexDisplay shows the last list page in Dex Web.
func (*SegmentsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSegments(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-segments": page}}, nil
}

func readSegments(ctx dex.Context) (constantcontact.SegmentsPage, error) {
	page, err := segmentsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return constantcontact.SegmentsPage{}, nil
	}
	return page, err
}

var segmentsAttribute = dex.DefineAttribute[constantcontact.SegmentsPage]("constantcontact-segments")

func mapSegmentsInput(input constantcontact.ListSegmentsInput) constantcontact.ListSegmentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested segment page to the provider Step."
type startSegments struct{ dex.StepDefaults }

func (startSegments) WaitFor(dex.Context, constantcontact.ListSegmentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSegments) Execute(_ dex.Context, input constantcontact.ListSegmentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[constantcontact.ListSegmentsInput](segmentsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the returned segment page and complete the Flow."
type completeSegments struct {
	dex.StepDefaultsNoWaitFor[constantcontact.ListSegmentsResult]
}

func (completeSegments) Execute(ctx dex.Context, result constantcontact.ListSegmentsResult) (*dex.StepDecision, error) {
	if err := segmentsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SegmentsFlow)(nil)
