// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package subscriberinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/kit/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SequencesFlowType is the sequence-directory example Flow identity.
const SequencesFlowType = "KitSequencesInspection"

const listSequencesStepType = "CallListSequences"

var sequencesPageAttribute = dex.DefineAttribute[kit.SequencesPage]("kit-sequences-page")

// SequencesFlow lists one page of Kit sequences.
type SequencesFlow struct {
	dex.FlowDefaults
	connection kit.Connection
}

// NewSequencesFlow binds the Kit connection at registration time.
func NewSequencesFlow(connection kit.Connection) *SequencesFlow {
	return &SequencesFlow{connection: connection}
}

func (*SequencesFlow) GetFlowType() string { return SequencesFlowType }

func (*SequencesFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sequencesPageAttribute}}
}

func (flow *SequencesFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSequences{}),
		dex.DefineStep(kit.NewListSequencesStep(kit.ListSequencesStepConfig[kit.ListSequencesInput]{
			StepType: listSequencesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Kit", Explanation: "List one page of sequences."},
			Connection:  flow.connection, MapToOperationInput: MapToListSequencesInput,
			Listed: sdkgo.GoTo(completeSequences{}),
		})),
		dex.DefineStep(completeSequences{}),
	}
}

func (flow *SequencesFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:kit-sequences-page value-type:json editable:false description:"Sequence page"
// GetDexSummary shows the sequence page in Dex Web.
func (*SequencesFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSequencesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-sequences-page": page}}, nil
}

// dex:field attribute-key:kit-sequences-page value-type:json editable:false description:"Sequence page"
// GetDexDisplay shows the sequence page in Dex Web.
func (*SequencesFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSequencesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-sequences-page": page}}, nil
}

func readSequencesPage(ctx dex.Context) (kit.SequencesPage, error) {
	page, err := sequencesPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return kit.SequencesPage{}, nil
	}
	return page, err
}

// MapToListSequencesInput passes the typed page request to Kit.
func MapToListSequencesInput(input kit.ListSequencesInput) kit.ListSequencesInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested sequence page to the provider Step."
type startSequences struct{ dex.StepDefaults }

func (startSequences) GetStepType() string { return "StartListSequences" }
func (startSequences) WaitFor(dex.Context, kit.ListSequencesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSequences) Execute(_ dex.Context, input kit.ListSequencesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[kit.ListSequencesInput](listSequencesStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one typed sequence page."
type completeSequences struct {
	dex.StepDefaultsNoWaitFor[kit.ListSequencesResult]
}

func (completeSequences) GetStepType() string { return "CompleteListSequences" }
func (completeSequences) Execute(ctx dex.Context, result kit.ListSequencesResult) (*dex.StepDecision, error) {
	if err := sequencesPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SequencesFlow)(nil)
