// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channelread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/buffer/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// IdeaFlowType is the stable example idea creation Flow identity.
const IdeaFlowType = "BufferIdeaCreate"

const ideaStepType = "CallCreateIdea"

var ideaAttribute = dex.DefineAttribute[buffer.IdeaProfile]("buffer-idea-created")

// IdeaFlow creates one Buffer idea from the typed start input.
type IdeaFlow struct {
	dex.FlowDefaults
	connection buffer.Connection
}

// NewIdeaFlow binds the provider connection at registration time.
func NewIdeaFlow(connection buffer.Connection) *IdeaFlow { return &IdeaFlow{connection: connection} }

func (*IdeaFlow) GetFlowType() string { return IdeaFlowType }

func (*IdeaFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{ideaAttribute}}
}

func (flow *IdeaFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startIdea{}),
		dex.DefineStep(buffer.NewCreateIdeaStep(buffer.CreateIdeaStepConfig[buffer.CreateIdeaInput]{
			StepType: ideaStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Buffer", Explanation: "Create an idea in the selected Buffer organization."},
			Connection:  flow.connection, MapToOperationInput: mapIdeaInput,
			Created: sdkgo.GoTo(completeIdea{}),
		})),
		dex.DefineStep(completeIdea{}),
	}
}

func (flow *IdeaFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:buffer-idea-created value-type:json editable:false description:"Created idea"
// GetDexSummary shows the created idea in Dex Web.
func (*IdeaFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	idea, err := readIdea(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"buffer-idea-created": idea}}, nil
}

// dex:field attribute-key:buffer-idea-created value-type:json editable:false description:"Created idea"
// GetDexDisplay shows the created idea in Dex Web.
func (*IdeaFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	idea, err := readIdea(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"buffer-idea-created": idea}}, nil
}

func readIdea(ctx dex.Context) (buffer.IdeaProfile, error) {
	idea, err := ideaAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return buffer.IdeaProfile{}, nil
	}
	return idea, err
}

func mapIdeaInput(input buffer.CreateIdeaInput) buffer.CreateIdeaInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed idea request to the Buffer provider Step."
type startIdea struct{ dex.StepDefaults }

func (startIdea) GetStepType() string { return "StartCreateIdea" }
func (startIdea) WaitFor(dex.Context, buffer.CreateIdeaInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startIdea) Execute(_ dex.Context, input buffer.CreateIdeaInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[buffer.CreateIdeaInput](ideaStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the created idea and complete the Flow."
type completeIdea struct {
	dex.StepDefaultsNoWaitFor[buffer.CreateIdeaResult]
}

func (completeIdea) GetStepType() string { return "CompleteCreateIdea" }
func (completeIdea) Execute(ctx dex.Context, result buffer.CreateIdeaResult) (*dex.StepDecision, error) {
	if err := ideaAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*IdeaFlow)(nil)
