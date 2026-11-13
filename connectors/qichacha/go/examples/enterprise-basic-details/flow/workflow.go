// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package enterprisebasicdetails runs Qichacha getEnterpriseBasicDetails as one Dex Flow.
package enterprisebasicdetails

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/qichacha/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "QichachaEnterpriseBasicDetails"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "qichacha"
	operationStepType = "CallGetEnterpriseBasicDetails"
)

var enterpriseBasicDetailsOutcomeAttribute = dex.DefineAttribute[qichacha.GetEnterpriseBasicDetailsOutput]("qichacha-enterprise-basic-details-outcome")

// Flow runs GetEnterpriseBasicDetails once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection qichacha.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection qichacha.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{enterpriseBasicDetailsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(qichacha.NewGetEnterpriseBasicDetailsStep(qichacha.GetEnterpriseBasicDetailsStepConfig[qichacha.GetEnterpriseBasicDetailsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Qichacha", Explanation: "Read registration fields for the requested mainland enterprise."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Fetched:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:qichacha-enterprise-basic-details-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readEnterpriseBasicDetailsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"qichacha-enterprise-basic-details-outcome": outcome}}, nil
}

// dex:field attribute-key:qichacha-enterprise-basic-details-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readEnterpriseBasicDetailsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"qichacha-enterprise-basic-details-outcome": outcome}}, nil
}

func readEnterpriseBasicDetailsOutcome(ctx dex.Context) (qichacha.GetEnterpriseBasicDetailsOutput, error) {
	outcome, err := enterpriseBasicDetailsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return qichacha.GetEnterpriseBasicDetailsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input qichacha.GetEnterpriseBasicDetailsInput) qichacha.GetEnterpriseBasicDetailsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetEnterpriseBasicDetails" }
func (startRequest) WaitFor(dex.Context, qichacha.GetEnterpriseBasicDetailsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input qichacha.GetEnterpriseBasicDetailsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[qichacha.GetEnterpriseBasicDetailsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[qichacha.GetEnterpriseBasicDetailsResult]
}

func (completeResult) GetStepType() string { return "CompleteGetEnterpriseBasicDetails" }
func (completeResult) Execute(ctx dex.Context, result qichacha.GetEnterpriseBasicDetailsResult) (*dex.StepDecision, error) {
	if err := enterpriseBasicDetailsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
