// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package identitylookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/ses/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// CreateFlowType is the stable Dex Web Start Flow identity for identity verification.
	CreateFlowType         = "AmazonSESCreateEmailIdentity"
	createIdentityStepType = "CallCreateEmailIdentity"
)

var createdIdentityAttribute = dex.DefineAttribute[ses.CreatedEmailIdentity]("ses-created-identity")

// CreateFlow starts verification through the example's SES connection.
type CreateFlow struct {
	dex.FlowDefaults
	connection ses.Connection
}

// NewCreateFlow binds the provider connection at registration time.
func NewCreateFlow(connection ses.Connection) *CreateFlow { return &CreateFlow{connection: connection} }

// GetFlowType returns the stable identity creation Flow identity.
func (*CreateFlow) GetFlowType() string { return CreateFlowType }

// GetPersistenceSchema registers the accepted identity metadata.
func (*CreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdIdentityAttribute}}
}

// GetRPCs exposes read-only Dex Web views of the identity metadata.
func (flow *CreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns identity metadata when this Flow has it.
//
// dex:field attribute-key:ses-created-identity value-type:json editable:false description:"SES identity verification"
func (*CreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	identity, err := readCreatedIdentity(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ses-created-identity": identity}}, nil
}

// GetDexDisplay returns identity metadata when this Flow has it.
//
// dex:field attribute-key:ses-created-identity value-type:json editable:false description:"SES identity verification"
func (*CreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	identity, err := readCreatedIdentity(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ses-created-identity": identity}}, nil
}

func readCreatedIdentity(ctx dex.Context) (ses.CreatedEmailIdentity, error) {
	identity, err := createdIdentityAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ses.CreatedEmailIdentity{}, nil
	}
	return identity, err
}

// GetSteps registers the start, provider create, and completion Steps.
func (flow *CreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startIdentityCreate{}),
		dex.DefineStep(ses.NewCreateEmailIdentityStep(ses.CreateEmailIdentityStepConfig[ses.CreateEmailIdentityInput]{
			StepType: createIdentityStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon SES", Explanation: "Start verification of an email address or domain."},
			Connection:  flow.connection, MapToOperationInput: mapToCreateIdentityInput,
			Created: sdkgo.GoTo(completeIdentityCreate{}),
		})),
		dex.DefineStep(completeIdentityCreate{}),
	}
}

func mapToCreateIdentityInput(input ses.CreateEmailIdentityInput) ses.CreateEmailIdentityInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested identity to the SES provider Step."
type startIdentityCreate struct{ dex.StepDefaults }

func (startIdentityCreate) GetStepType() string { return "StartCreateEmailIdentity" }
func (startIdentityCreate) WaitFor(dex.Context, ses.CreateEmailIdentityInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startIdentityCreate) Execute(_ dex.Context, input ses.CreateEmailIdentityInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ses.CreateEmailIdentityInput](createIdentityStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with SES identity verification metadata."
type completeIdentityCreate struct {
	dex.StepDefaultsNoWaitFor[ses.CreateEmailIdentityResult]
}

func (completeIdentityCreate) GetStepType() string { return "CompleteCreateEmailIdentity" }
func (completeIdentityCreate) Execute(ctx dex.Context, result ses.CreateEmailIdentityResult) (*dex.StepDecision, error) {
	if err := createdIdentityAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateFlow)(nil)
