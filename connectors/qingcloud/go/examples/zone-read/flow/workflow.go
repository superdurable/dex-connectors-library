// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package zoneread runs QingCloud describeZones as one Dex Flow.
package zoneread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/qingcloud/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "QingCloudZoneRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "qingcloud"
	operationStepType = "CallDescribeZones"
)

var zonesAttribute = dex.DefineAttribute[qingcloud.ZonesPage]("qingcloud-zone-read")

// Flow runs DescribeZones once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection qingcloud.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection qingcloud.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{zonesAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(qingcloud.NewDescribeZonesStep(qingcloud.DescribeZonesStepConfig[qingcloud.DescribeZonesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "QingCloud", Explanation: "List accessible QingCloud IaaS zones."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:qingcloud-zone-read value-type:json editable:false description:"Accessible IaaS zones"
// GetDexSummary shows the last accessible IaaS zones in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readZones(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"qingcloud-zone-read": outcome}}, nil
}

// dex:field attribute-key:qingcloud-zone-read value-type:json editable:false description:"Accessible IaaS zones"
// GetDexDisplay shows the last accessible IaaS zones in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readZones(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"qingcloud-zone-read": outcome}}, nil
}

func readZones(ctx dex.Context) (qingcloud.ZonesPage, error) {
	outcome, err := zonesAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return qingcloud.ZonesPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input qingcloud.DescribeZonesInput) qingcloud.DescribeZonesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartDescribeZones" }
func (startRequest) WaitFor(dex.Context, qingcloud.DescribeZonesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input qingcloud.DescribeZonesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[qingcloud.DescribeZonesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[qingcloud.DescribeZonesResult]
}

func (completeResult) GetStepType() string { return "CompleteDescribeZones" }
func (completeResult) Execute(ctx dex.Context, result qingcloud.DescribeZonesResult) (*dex.StepDecision, error) {
	if err := zonesAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
