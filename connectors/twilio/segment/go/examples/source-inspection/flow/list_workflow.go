// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sourceinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/twilio/segment/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const sourceListFlowType = "SegmentSourceListing"
const sourceListStepType = "CallListSources"

var sourcePageAttribute = dex.DefineAttribute[segment.SourcePage]("segment-source-page")

// SourceListFlow lists one page of Source identities.
type SourceListFlow struct {
	dex.FlowDefaults
	connection segment.Connection
}

// NewSourceListFlow binds a Segment connection.
func NewSourceListFlow(connection segment.Connection) *SourceListFlow {
	return &SourceListFlow{connection: connection}
}

// GetFlowType returns the stable Source listing identity.
func (*SourceListFlow) GetFlowType() string { return sourceListFlowType }

// GetSteps registers the list request and completion Steps.
func (flow *SourceListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSourceList{}),
		dex.DefineStep(segment.NewListSourcesStep(segment.ListSourcesStepConfig[segment.ListSourcesInput]{
			StepType: sourceListStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "segment-list", GroupLabel: "Segment Sources", Explanation: "List Source identities without write keys."},
			MapToOperationInput: func(input segment.ListSourcesInput) segment.ListSourcesInput { return input },
			Listed:              sdkgo.GoTo(completeSourceList{}),
		})),
		dex.DefineStep(completeSourceList{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *SourceListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the Source page Attribute.
func (*SourceListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sourcePageAttribute}}
}

// GetDexSummary returns the saved Source page.
// dex:field attribute-key:segment-source-page value-type:json editable:false description:"Source identity page"
func (*SourceListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSourcePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"segment-source-page": page}}, nil
}

// GetDexDisplay returns the saved Source page.
// dex:field attribute-key:segment-source-page value-type:json editable:false description:"Source identity page"
func (*SourceListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSourcePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"segment-source-page": page}}, nil
}

func readSourcePage(ctx dex.Context) (segment.SourcePage, error) {
	page, err := sourcePageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return segment.SourcePage{}, nil
	}
	return page, err
}

// dex:group group-id:segment-list-start group-label:"Start Source list"
// dex:explanation text:"Pass the Source pagination input to the connector Step."
type startSourceList struct{ dex.StepDefaults }

func (startSourceList) GetStepType() string { return "StartSourceList" }
func (startSourceList) WaitFor(dex.Context, segment.ListSourcesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSourceList) Execute(_ dex.Context, input segment.ListSourcesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[segment.ListSourcesInput](sourceListStepType), input), nil
}

// dex:group group-id:segment-list-complete group-label:"Complete Source list"
// dex:explanation text:"Persist and return the Source page."
type completeSourceList struct {
	dex.StepDefaultsNoWaitFor[segment.ListSourcesResult]
}

func (completeSourceList) GetStepType() string { return "CompleteSourceList" }
func (completeSourceList) Execute(ctx dex.Context, result segment.ListSourcesResult) (*dex.StepDecision, error) {
	if err := sourcePageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SourceListFlow)(nil)
