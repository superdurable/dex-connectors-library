// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package queueinspection reads one RabbitMQ queue's metrics as a Dex Flow.
package queueinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/rabbitmq/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "RabbitMQQueueInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "rabbitmq"
	operationStepType = "CallGetQueueMetrics"
)

var metricsAttribute = dex.DefineAttribute[rabbitmq.QueueMetrics]("rabbitmq-queue-inspection")

// Flow runs GetQueueMetrics once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection rabbitmq.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection rabbitmq.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{metricsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(rabbitmq.NewGetQueueMetricsStep(rabbitmq.GetQueueMetricsStepConfig[rabbitmq.GetQueueMetricsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "RabbitMQ", Explanation: "Read current metrics for one named queue."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:rabbitmq-queue-inspection value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readMetrics(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"rabbitmq-queue-inspection": outcome}}, nil
}

// dex:field attribute-key:rabbitmq-queue-inspection value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readMetrics(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"rabbitmq-queue-inspection": outcome}}, nil
}

func readMetrics(ctx dex.Context) (rabbitmq.QueueMetrics, error) {
	outcome, err := metricsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return rabbitmq.QueueMetrics{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input rabbitmq.GetQueueMetricsInput) rabbitmq.GetQueueMetricsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetQueueMetrics" }
func (startRequest) WaitFor(dex.Context, rabbitmq.GetQueueMetricsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input rabbitmq.GetQueueMetricsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[rabbitmq.GetQueueMetricsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[rabbitmq.GetQueueMetricsResult]
}

func (completeResult) GetStepType() string { return "CompleteGetQueueMetrics" }
func (completeResult) Execute(ctx dex.Context, result rabbitmq.GetQueueMetricsResult) (*dex.StepDecision, error) {
	if err := metricsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
