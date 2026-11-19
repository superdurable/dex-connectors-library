// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datadoglookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/datadog/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// MonitorInspectionFlowType is the stable identity of the monitor inspection example.
const MonitorInspectionFlowType = "DatadogMonitorInspection"

var inspectedMonitorAttribute = dex.DefineAttribute[datadog.Monitor]("datadog-inspected-monitor")

// MonitorInspectionFlow reads one monitor by ID through a Connector Step.
type MonitorInspectionFlow struct {
	dex.FlowDefaults
	connection datadog.Connection
}

// NewMonitorInspectionFlow binds the static connection for monitor inspection.
func NewMonitorInspectionFlow(connection datadog.Connection) *MonitorInspectionFlow {
	return &MonitorInspectionFlow{connection: connection}
}

// GetFlowType returns the stable monitor inspection Flow identity.
func (*MonitorInspectionFlow) GetFlowType() string { return MonitorInspectionFlowType }

// GetPersistenceSchema registers the last inspected monitor.
func (*MonitorInspectionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{inspectedMonitorAttribute}}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *MonitorInspectionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary shows the selected monitor after the provider Step completes.
// dex:field attribute-key:datadog-inspected-monitor value-type:json editable:false description:"Selected monitor"
func (*MonitorInspectionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	monitor, err := optionalAttribute(ctx, inspectedMonitorAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"datadog-inspected-monitor": monitor}}, nil
}

// GetDexDisplay shows the selected monitor after the provider Step completes.
// dex:field attribute-key:datadog-inspected-monitor value-type:json editable:false description:"Selected monitor"
func (*MonitorInspectionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	monitor, err := optionalAttribute(ctx, inspectedMonitorAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"datadog-inspected-monitor": monitor}}, nil
}

// GetSteps registers the start, provider call, and completion steps.
func (flow *MonitorInspectionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMonitorInspection{}),
		dex.DefineStep(datadog.NewGetMonitorStep(datadog.GetMonitorStepConfig[datadog.GetMonitorInput]{
			StepType: "GetDatadogMonitor", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Datadog", Explanation: "Read one Datadog monitor."},
			Connection:  flow.connection, MapToOperationInput: mapMonitorInput,
			Found: sdkgo.GoTo(completeMonitorInspection{}),
		})),
		dex.DefineStep(completeMonitorInspection{}),
	}
}

func mapMonitorInput(input datadog.GetMonitorInput) datadog.GetMonitorInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the monitor ID to the provider Step."
type startMonitorInspection struct{ dex.StepDefaults }

func (startMonitorInspection) GetStepType() string { return "StartMonitorInspection" }
func (startMonitorInspection) WaitFor(dex.Context, datadog.GetMonitorInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMonitorInspection) Execute(_ dex.Context, input datadog.GetMonitorInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[datadog.GetMonitorInput]("GetDatadogMonitor"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the monitor summary and complete the Flow."
type completeMonitorInspection struct {
	dex.StepDefaultsNoWaitFor[datadog.GetMonitorResult]
}

func (completeMonitorInspection) GetStepType() string { return "CompleteMonitorInspection" }
func (completeMonitorInspection) Execute(ctx dex.Context, result datadog.GetMonitorResult) (*dex.StepDecision, error) {
	if err := inspectedMonitorAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*MonitorInspectionFlow)(nil)
