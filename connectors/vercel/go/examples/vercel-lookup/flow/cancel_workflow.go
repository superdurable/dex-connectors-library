// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vercellookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/vercel/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CancelFlowType identifies the deployment cancellation example.
const CancelFlowType = "VercelCancelDeployment"

var canceledDeploymentAttribute = dex.DefineAttribute[vercel.Deployment]("vercel-canceled-deployment")

// CancelFlow cancels one deployment with the static Vercel connection.
type CancelFlow struct {
	dex.FlowDefaults
	connection vercel.Connection
}

// NewCancelFlow binds the Vercel connection at registration time.
func NewCancelFlow(connection vercel.Connection) *CancelFlow {
	return &CancelFlow{connection: connection}
}
func (*CancelFlow) GetFlowType() string { return CancelFlowType }
func (*CancelFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{canceledDeploymentAttribute}}
}

func (flow *CancelFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCancellation{}),
		dex.DefineStep(vercel.NewCancelDeploymentStep(vercel.CancelDeploymentStepConfig[vercel.CancelDeploymentInput]{
			StepType: "CallCancelDeployment", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "vercel", GroupLabel: "Vercel", Explanation: "Cancel one Vercel deployment."},
			MapToOperationInput: mapCancellationInput, Canceled: sdkgo.GoTo(completeCancellation{}),
		})),
		dex.DefineStep(completeCancellation{}),
	}
}

// GetRPCs exposes the canceled deployment to Dex Web.
func (flow *CancelFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:vercel-canceled-deployment value-type:json editable:false description:"Canceled deployment"
// GetDexSummary returns the last canceled deployment.
func (*CancelFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := optionalAttribute(ctx, canceledDeploymentAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"vercel-canceled-deployment": value}}, nil
}

// dex:field attribute-key:vercel-canceled-deployment value-type:json editable:false description:"Canceled deployment"
// GetDexDisplay returns the last canceled deployment.
func (*CancelFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := optionalAttribute(ctx, canceledDeploymentAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"vercel-canceled-deployment": value}}, nil
}

func mapCancellationInput(input vercel.CancelDeploymentInput) vercel.CancelDeploymentInput {
	return input
}

// dex:group group-id:vercel group-label:"Vercel"
// dex:explanation text:"Pass the typed deployment ID to the cancellation Step."
type startCancellation struct{ dex.StepDefaults }

func (startCancellation) GetStepType() string { return "StartVercelCancellation" }
func (startCancellation) WaitFor(dex.Context, vercel.CancelDeploymentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCancellation) Execute(_ dex.Context, input vercel.CancelDeploymentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[vercel.CancelDeploymentInput]("CallCancelDeployment"), input), nil
}

// dex:group group-id:vercel group-label:"Vercel"
// dex:explanation text:"Complete with the canceled deployment metadata."
type completeCancellation struct {
	dex.StepDefaultsNoWaitFor[vercel.CancelDeploymentResult]
}

func (completeCancellation) GetStepType() string { return "CompleteVercelCancellation" }
func (completeCancellation) Execute(ctx dex.Context, result vercel.CancelDeploymentResult) (*dex.StepDecision, error) {
	if err := canceledDeploymentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CancelFlow)(nil)
