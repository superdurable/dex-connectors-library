// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datadoglookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/datadog/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// MonitorCreationFlowType is the stable identity of the monitor creation example.
const MonitorCreationFlowType = "DatadogMonitorCreation"

var createdMonitorAttribute = dex.DefineAttribute[datadog.Monitor]("datadog-created-monitor")

// MonitorCreationFlow creates one metric alert monitor.
type MonitorCreationFlow struct {
	dex.FlowDefaults
	connection datadog.Connection
}

// NewMonitorCreationFlow binds a Datadog connection to the creation Flow.
func NewMonitorCreationFlow(connection datadog.Connection) *MonitorCreationFlow {
	return &MonitorCreationFlow{connection: connection}
}

func (*MonitorCreationFlow) GetFlowType() string { return MonitorCreationFlowType }
func (*MonitorCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdMonitorAttribute}}
}
func (flow *MonitorCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMonitorCreation{}),
		dex.DefineStep(datadog.NewCreateMonitorStep(datadog.CreateMonitorStepConfig[datadog.CreateMonitorInput]{
			StepType: "CallCreateMonitor", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Datadog", Explanation: "Create a metric alert monitor with bounded sends."},
			Connection:          flow.connection,
			MapToOperationInput: func(input datadog.CreateMonitorInput) datadog.CreateMonitorInput { return input },
			Created:             sdkgo.GoTo(completeMonitorCreation{}),
		})),
		dex.DefineStep(completeMonitorCreation{}),
	}
}
func (flow *MonitorCreationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:datadog-created-monitor value-type:json editable:false description:"Created monitor identity"
// GetDexSummary shows the confirmed monitor identity.
func (*MonitorCreationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	monitor, err := readCreatedMonitor(ctx)
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"datadog-created-monitor": monitor}}, err
}

// dex:field attribute-key:datadog-created-monitor value-type:json editable:false description:"Created monitor identity"
// GetDexDisplay shows the confirmed monitor identity.
func (*MonitorCreationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	monitor, err := readCreatedMonitor(ctx)
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"datadog-created-monitor": monitor}}, err
}

func readCreatedMonitor(ctx dex.Context) (datadog.Monitor, error) {
	monitor, err := createdMonitorAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return datadog.Monitor{}, nil
	}
	return monitor, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass explicit metric alert settings to the provider Step."
type startMonitorCreation struct{ dex.StepDefaults }

func (startMonitorCreation) GetStepType() string { return "StartMonitorCreation" }
func (startMonitorCreation) WaitFor(dex.Context, datadog.CreateMonitorInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMonitorCreation) Execute(_ dex.Context, input datadog.CreateMonitorInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[datadog.CreateMonitorInput]("CallCreateMonitor"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the confirmed monitor identity and complete the Flow."
type completeMonitorCreation struct {
	dex.StepDefaultsNoWaitFor[datadog.CreateMonitorResult]
}

func (completeMonitorCreation) GetStepType() string { return "CompleteMonitorCreation" }
func (completeMonitorCreation) Execute(ctx dex.Context, result datadog.CreateMonitorResult) (*dex.StepDecision, error) {
	if err := createdMonitorAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*MonitorCreationFlow)(nil)
